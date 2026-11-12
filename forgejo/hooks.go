package forgejo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/unsupported"
)

type hookJSON struct {
	ID                  int64           `json:"id"`
	Type                string          `json:"type"`
	URL                 string          `json:"url"`
	ContentType         string          `json:"content_type"`
	Metadata            json.RawMessage `json:"metadata"`
	Events              []string        `json:"events"`
	Active              bool            `json:"active"`
	BranchFilter        string          `json:"branch_filter"`
	AuthorizationHeader string          `json:"authorization_header"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

func (h *hookJSON) toWebhook() gitoperationslib.Webhook {
	hook := gitoperationslib.Webhook{
		ID:                     h.ID,
		Type:                   gitoperationslib.HookType(h.Type),
		URL:                    h.URL,
		ContentType:            gitoperationslib.HookContentType(h.ContentType),
		Events:                 fromForgejoEvents(h.Events),
		Active:                 h.Active,
		BranchFilter:           h.BranchFilter,
		HasAuthorizationHeader: h.AuthorizationHeader != "",
		CreatedAt:              h.CreatedAt,
		UpdatedAt:              h.UpdatedAt,
	}
	if hook.Type == gitoperationslib.HookTypeSlack {
		var meta struct {
			Channel  string `json:"channel"`
			Username string `json:"username"`
			IconURL  string `json:"icon_url"`
			Color    string `json:"color"`
		}
		if json.Unmarshal(h.Metadata, &meta) == nil {
			hook.Slack = &gitoperationslib.SlackHookSettings{Channel: meta.Channel, Username: meta.Username, IconURL: meta.IconURL, Color: meta.Color}
		}
	}
	return hook
}

// Forgejo reports GitHub's pull_request_review as one event per verdict, but
// only accepts it under that one name: a verdict's own name is dropped.
var reviewVerdictEvents = []string{"pull_request_review_approved", "pull_request_review_rejected"}

var hookStatuses = []string{"blocked", "cancelled", "failure", "running", "skipped", "success", "waiting"}

func statusEvents(prefix string) []string {
	out := make([]string, len(hookStatuses))
	for i, s := range hookStatuses {
		out[i] = prefix + "_" + s
	}
	return out
}

var workflowRunEvents, workflowJobEvents = statusEvents("workflow_run"), statusEvents("workflow_job")

// workflowEventsVersion is the first release with workflow_* events.
var workflowEventsVersion = Version{17, 0, 0}

// allHookEvents is what HookEventAll sends: Forgejo has no wildcard, and takes
// "*" as an unknown event, leaving a webhook subscribed to nothing. A name the
// server drops here is not reported: see requestedEvents.
var allHookEvents = []string{
	"create", "delete", "fork", "push", "issues", "issue_assign", "issue_label", "issue_milestone", "issue_comment",
	"pull_request", "pull_request_assign", "pull_request_label", "pull_request_milestone", "pull_request_comment",
	"pull_request_review", "pull_request_review_request", "pull_request_sync", "wiki", "repository", "release", "package",
}

// oldWorkflowEvents are what versions before 17 have for workflows. 17 deprecates
// them, and both sets fire for one run there, so HookEventAll sends only one.
var oldWorkflowEvents = []string{"action_run_failure", "action_run_recover", "action_run_success"}

func (c *Client) toForgejoEvents(events []gitoperationslib.HookEvent, op string) ([]string, error) {
	hasWorkflows := c.version.Compare(workflowEventsVersion) >= 0
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e {
		case gitoperationslib.HookEventWorkflowRun, gitoperationslib.HookEventWorkflowJob:
			if !hasWorkflows {
				return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, fmt.Sprintf("event %q before Forgejo %s", e, workflowEventsVersion))
			}
			if e == gitoperationslib.HookEventWorkflowRun {
				out = append(out, workflowRunEvents...)
			} else {
				out = append(out, workflowJobEvents...)
			}
		case gitoperationslib.HookEventAll:
			out = append(out, allHookEvents...)
			if hasWorkflows {
				out = append(append(out, workflowRunEvents...), workflowJobEvents...)
			} else {
				out = append(out, oldWorkflowEvents...)
			}
		default:
			out = append(out, string(e))
		}
	}
	return out, nil
}

// requestedEvents is what checkEvents holds the server to: the events the caller
// named as the server reports them, not those HookEventAll stands for.
func requestedEvents(events []gitoperationslib.HookEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e {
		case gitoperationslib.HookEventAll:
		case gitoperationslib.HookEventPullRequestReview:
			out = append(out, reviewVerdictEvents...)
		case gitoperationslib.HookEventWorkflowRun:
			out = append(out, workflowRunEvents...)
		case gitoperationslib.HookEventWorkflowJob:
			out = append(out, workflowJobEvents...)
		case "issues_only":
			out = append(out, "issues")
		case "pull_request_only":
			out = append(out, "pull_request")
		default:
			out = append(out, string(e))
		}
	}
	return out
}

func fromForgejoEvents(names []string) []gitoperationslib.HookEvent {
	out := make([]gitoperationslib.HookEvent, 0, len(names))
	add := func(e gitoperationslib.HookEvent) {
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	has := func(group []string) bool {
		return !slices.ContainsFunc(group, func(n string) bool { return !slices.Contains(names, n) })
	}
	for _, n := range names {
		switch {
		case slices.Contains(reviewVerdictEvents, n):
			add(gitoperationslib.HookEventPullRequestReview)
		case slices.Contains(workflowRunEvents, n) && has(workflowRunEvents):
			add(gitoperationslib.HookEventWorkflowRun)
		case slices.Contains(workflowJobEvents, n) && has(workflowJobEvents):
			add(gitoperationslib.HookEventWorkflowJob)
		default:
			add(gitoperationslib.HookEvent(n))
		}
	}
	return out
}

// fixedEvent reports an event a PATCH cannot change: editHook never reads these,
// and answers 200 all the same, so they stay as the hook was created (checked on
// 15.0.11, 16.0.7 and 17.0.1).
func fixedEvent(name string) bool {
	return name == "package" || strings.HasPrefix(name, "action_run_") ||
		strings.HasPrefix(name, "workflow_run_") || strings.HasPrefix(name, "workflow_job_")
}

// fixedEventChanges names what a PATCH to requested could not do to a hook on
// current.
func fixedEventChanges(current, requested []string) (added, removed []string) {
	for _, e := range requested {
		if fixedEvent(e) && !slices.Contains(current, e) && !slices.Contains(added, e) {
			added = append(added, e)
		}
	}
	for _, e := range current {
		if fixedEvent(e) && !slices.Contains(requested, e) {
			removed = append(removed, e)
		}
	}
	return added, removed
}

// keepEvents turns the events a hook reports into what makes a PATCH leave them
// as they are. Forgejo reports "issues" for the issue event alone, but takes that
// name as all of them; likewise "pull_request". A verdict's name is dropped, and
// pull_request_review_comment comes with pull_request_comment.
func keepEvents(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		switch {
		case n == "issues":
			n = "issues_only"
		case n == "pull_request":
			n = "pull_request_only"
		case slices.Contains(reviewVerdictEvents, n):
			n = "pull_request_review"
		case n == "pull_request_review_comment":
			continue
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// checkEvents reports requested events the server dropped (Forgejo ignores
// unknown names instead of rejecting them).
func checkEvents(hook *gitoperationslib.Webhook, requested, got []string, op string) (*gitoperationslib.Webhook, error) {
	var missing []string
	for _, e := range requested {
		if !slices.Contains(got, e) {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		return hook, fmt.Errorf("forgejo: %s: server dropped unknown events %s (hook %d active=%v)",
			op, strings.Join(missing, ", "), hook.ID, hook.Active)
	}
	return hook, nil
}

type hookBody struct {
	Type                string            `json:"type,omitempty"`
	Config              map[string]string `json:"config,omitempty"`
	Events              []string          `json:"events,omitempty"`
	Active              *bool             `json:"active,omitempty"`
	BranchFilter        *string           `json:"branch_filter,omitempty"`
	AuthorizationHeader *string           `json:"authorization_header,omitempty"`
}

// hookService implements webhook CRUD below base ("repos/o/r/hooks" or
// "orgs/o/hooks").
type hookService struct {
	c    *Client
	base string
}

func (s hookService) path(id int64) string { return s.base + "/" + strconv.FormatInt(id, 10) }

func (s hookService) list(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	var out []hookJSON
	resp, err := s.c.api.Do(ctx, http.MethodGet, s.base, forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, err
	}
	return toPage(resp, out, (*hookJSON).toWebhook), nil
}

func (s hookService) get(ctx context.Context, id int64) (*gitoperationslib.Webhook, error) {
	var out hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodGet, s.path(id), nil, nil, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return &hook, nil
}

func (s hookService) create(ctx context.Context, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	return s.createWith(ctx, opts, nil)
}

// createWith adds extra to the hook's config.
func (s hookService) createWith(ctx context.Context, opts gitoperationslib.CreateWebhookOptions, extra map[string]string) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.create"
	if opts.URL == "" || len(opts.Events) == 0 {
		return nil, errors.New("forgejo: webhooks.create: url and events are required")
	}
	if opts.InsecureSSL {
		return nil, insecureSSLNotSupported(op)
	}
	hookType := opts.Type
	if hookType == "" {
		hookType = gitoperationslib.HookTypeForgejo
	}
	switch hookType {
	case gitoperationslib.HookTypeTelegram, gitoperationslib.HookTypeMatrix, gitoperationslib.HookTypePackagist, gitoperationslib.HookTypeSourceHutBuilds:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, fmt.Sprintf("creating %s webhooks, whose settings its API drops", hookType))
	}
	if (hookType == gitoperationslib.HookTypeSlack) != (opts.Slack != nil) {
		return nil, errors.New("forgejo: webhooks.create: Slack settings go with HookTypeSlack, and it needs them")
	}
	events, err := s.c.toForgejoEvents(opts.Events, op)
	if err != nil {
		return nil, err
	}
	ct := opts.ContentType
	if ct == "" {
		ct = gitoperationslib.HookContentJSON
	}
	active := true
	if opts.Active != nil {
		active = *opts.Active
	}
	body := hookBody{
		Type:                string(hookType),
		Config:              map[string]string{"url": opts.URL, "content_type": string(ct)},
		Events:              events,
		Active:              &active,
		BranchFilter:        nonEmpty(opts.BranchFilter),
		AuthorizationHeader: nonEmpty(opts.AuthorizationHeader),
	}
	if opts.Secret != "" {
		body.Config["secret"] = opts.Secret
	}
	addSlackConfig(body.Config, opts.Slack)
	for k, v := range extra {
		body.Config[k] = v
	}
	var out hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodPost, s.base, nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return checkEvents(&hook, requestedEvents(opts.Events), out.Events, op)
}

// insecureSSLNotSupported refuses what Forgejo would drop silently: skipping the
// certificate check is its SKIP_TLS_VERIFY setting, for the whole instance.
func insecureSSLNotSupported(op string) error {
	return gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "skipping the certificate check for one webhook")
}

func addSlackConfig(config map[string]string, slack *gitoperationslib.SlackHookSettings) {
	if slack != nil {
		config["channel"], config["username"], config["icon_url"], config["color"] = slack.Channel, slack.Username, slack.IconURL, slack.Color
	}
}

func (s hookService) edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.edit"
	switch {
	case opts.InsecureSSL != nil && *opts.InsecureSSL:
		return nil, insecureSSLNotSupported(op)
	case opts.Secret != nil:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "changing a webhook's secret, which its API ignores: create a new webhook")
	case opts.AuthorizationHeader != nil && *opts.AuthorizationHeader == "":
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "removing a webhook's authorization header, which its API ignores")
	case opts.Events != nil && len(opts.Events) == 0:
		return nil, errors.New("forgejo: webhooks.edit: Events must not be empty")
	case opts.Slack != nil && opts.Slack.Channel == "":
		return nil, errors.New("forgejo: webhooks.edit: Slack settings need a channel")
	}
	events, err := s.c.toForgejoEvents(opts.Events, op)
	if err != nil {
		return nil, err
	}
	// A PATCH without events or a branch filter resets them, so read what it
	// must keep. A hook subscribed to nothing still ends up with "push".
	var cur hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodGet, s.path(id), nil, nil, &cur); err != nil {
		return nil, err
	}
	if opts.Slack != nil && cur.Type != string(gitoperationslib.HookTypeSlack) {
		return nil, errors.New("forgejo: webhooks.edit: Slack settings go with HookTypeSlack")
	}
	if opts.Events == nil {
		events = keepEvents(cur.Events)
	} else if added, removed := fixedEventChanges(cur.Events, events); len(added)+len(removed) > 0 {
		var changes []string
		if len(added) > 0 {
			changes = append(changes, "adding "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			changes = append(changes, "removing "+strings.Join(removed, ", "))
		}
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op,
			strings.Join(changes, " and ")+" on an existing webhook, which its API ignores: leave these as they are in Events, or create a new webhook and delete this one")
	}
	body := hookBody{
		Active:              opts.Active,
		BranchFilter:        cmp.Or(opts.BranchFilter, &cur.BranchFilter),
		AuthorizationHeader: opts.AuthorizationHeader,
		Events:              events,
	}
	config := map[string]string{}
	if opts.URL != nil {
		config["url"] = *opts.URL
	}
	if opts.ContentType != nil {
		config["content_type"] = string(*opts.ContentType)
	}
	addSlackConfig(config, opts.Slack)
	if len(config) > 0 {
		body.Config = config
	}
	var out hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodPatch, s.path(id), nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return checkEvents(&hook, requestedEvents(opts.Events), out.Events, op)
}

func (s hookService) delete(ctx context.Context, id int64) error {
	_, err := s.c.api.Do(ctx, http.MethodDelete, s.path(id), nil, nil, nil)
	return err
}

// Webhooks implements gitoperationslib.Client.
func (c *Client) Webhooks() gitoperationslib.Webhooks {
	return webhooks{c, unsupported.Webhooks{Backend: gitoperationslib.BackendForgejo, Reason: "Forgejo's API keeps no delivery history"}}
}

// webhooks has no delivery history: Forgejo's API has none.
type webhooks struct {
	c *Client
	unsupported.Webhooks
}

func (w webhooks) service(scope gitoperationslib.HookScope) (hookService, error) {
	switch {
	case scope.Owner == "":
		return hookService{}, errors.New("forgejo: webhooks: scope owner is required")
	case scope.Repo == "":
		return hookService{w.c, forgejoapi.Path("orgs", scope.Owner, "hooks")}, nil
	}
	return hookService{w.c, forgejoapi.Path("repos", scope.Owner, scope.Repo, "hooks")}, nil
}

func (w webhooks) List(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	s, err := w.service(scope)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, err
	}
	return s.list(ctx, opts)
}

func (w webhooks) Get(ctx context.Context, scope gitoperationslib.HookScope, id int64) (*gitoperationslib.Webhook, error) {
	s, err := w.service(scope)
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (w webhooks) Create(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	s, err := w.service(scope)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, opts)
}

func (w webhooks) Edit(ctx context.Context, scope gitoperationslib.HookScope, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	s, err := w.service(scope)
	if err != nil {
		return nil, err
	}
	return s.edit(ctx, id, opts)
}

func (w webhooks) Delete(ctx context.Context, scope gitoperationslib.HookScope, id int64) error {
	s, err := w.service(scope)
	if err != nil {
		return err
	}
	return s.delete(ctx, id)
}

func (w webhooks) Ping(ctx context.Context, scope gitoperationslib.HookScope, id int64) error {
	s, err := w.service(scope)
	if err != nil {
		return err
	}
	if scope.Repo == "" {
		return gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "webhooks.ping", "test deliveries for organisation hooks")
	}
	_, err = w.c.api.Do(ctx, http.MethodPost, s.path(id)+"/tests", nil, nil, nil)
	return err
}

var _ gitoperationslib.Site = site{}

// Site implements gitoperationslib.Client.
func (c *Client) Site() gitoperationslib.Site { return site{c} }

type site struct{ c *Client }

func (svc account) Webhooks() gitoperationslib.UserWebhooks {
	c := svc.c
	return userHooks{hookService{c, "user/hooks"}}
}

type userHooks struct{ s hookService }

func (u userHooks) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	return u.s.list(ctx, opts)
}

func (u userHooks) Get(ctx context.Context, id int64) (*gitoperationslib.Webhook, error) {
	return u.s.get(ctx, id)
}

func (u userHooks) Create(ctx context.Context, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	return u.s.create(ctx, opts)
}

func (u userHooks) Edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	return u.s.edit(ctx, id, opts)
}

func (u userHooks) Delete(ctx context.Context, id int64) error { return u.s.delete(ctx, id) }

func (svc site) SystemWebhooks() gitoperationslib.SystemWebhooks {
	c := svc.c
	return systemHooks{hookService{c, "admin/hooks"}}
}

func (svc site) DefaultWebhooks() gitoperationslib.DefaultWebhooks {
	c := svc.c
	return defaultHooks{hookService{c, "admin/hooks"}}
}

// isSystemHook tells the two kinds apart: GET /admin/hooks lists system hooks
// only, while /admin/hooks/{id} takes both and its answers look the same
// (checked on 15.0.11).
func isSystemHook(ctx context.Context, s hookService, id int64) (bool, error) {
	for hook, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, s.list) {
		if err != nil {
			return false, err
		}
		if hook.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func wrongHookKind(id int64, kind string) error {
	return &gitoperationslib.APIError{Backend: gitoperationslib.BackendForgejo, StatusCode: http.StatusNotFound, Message: fmt.Sprintf("webhook %d is not a %s webhook", id, kind)}
}

type systemHooks struct{ s hookService }

func (h systemHooks) check(ctx context.Context, id int64) error {
	system, err := isSystemHook(ctx, h.s, id)
	if err == nil && !system {
		return wrongHookKind(id, "system")
	}
	return err
}

func (h systemHooks) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	return h.s.list(ctx, opts)
}

func (h systemHooks) Get(ctx context.Context, id int64) (*gitoperationslib.Webhook, error) {
	if err := h.check(ctx, id); err != nil {
		return nil, err
	}
	return h.s.get(ctx, id)
}

// Create depends on an undocumented flag (checked on 15.0.11 and 17.0.1, against
// a hook made in the web interface). Without it, the server makes a default hook,
// which is copied into every new repository, so do not leave one behind.
func (h systemHooks) Create(ctx context.Context, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	hook, err := h.s.createWith(ctx, opts, map[string]string{"is_system_webhook": "true"})
	if hook == nil {
		return nil, err
	}
	system, checkErr := isSystemHook(ctx, h.s, hook.ID)
	if checkErr != nil {
		return hook, fmt.Errorf("forgejo: webhooks.create: webhook %d created, but its kind is unknown: %w", hook.ID, checkErr)
	}
	if !system {
		if delErr := h.s.delete(ctx, hook.ID); delErr != nil {
			return hook, fmt.Errorf("forgejo: webhooks.create: the server made webhook %d a default webhook, and deleting it failed: %w", hook.ID, delErr)
		}
		return nil, fmt.Errorf("forgejo: webhooks.create: the server made a default webhook instead of a system one; it was deleted")
	}
	return hook, err
}

func (h systemHooks) Edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	if err := h.check(ctx, id); err != nil {
		return nil, err
	}
	return h.s.edit(ctx, id, opts)
}

func (h systemHooks) Delete(ctx context.Context, id int64) error {
	if err := h.check(ctx, id); err != nil {
		return err
	}
	return h.s.delete(ctx, id)
}

type defaultHooks struct{ s hookService }

func (h defaultHooks) check(ctx context.Context, id int64) error {
	system, err := isSystemHook(ctx, h.s, id)
	if err == nil && system {
		return wrongHookKind(id, "default")
	}
	return err
}

func (h defaultHooks) Create(ctx context.Context, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	return h.s.create(ctx, opts)
}

func (h defaultHooks) Get(ctx context.Context, id int64) (*gitoperationslib.Webhook, error) {
	if err := h.check(ctx, id); err != nil {
		return nil, err
	}
	return h.s.get(ctx, id)
}

func (h defaultHooks) Edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	if err := h.check(ctx, id); err != nil {
		return nil, err
	}
	return h.s.edit(ctx, id, opts)
}

func (h defaultHooks) Delete(ctx context.Context, id int64) error {
	if err := h.check(ctx, id); err != nil {
		return err
	}
	return h.s.delete(ctx, id)
}
