package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type hookJSON struct {
	ID           int64             `json:"id"`
	Config       map[string]string `json:"config"`
	Events       []string          `json:"events"`
	Active       bool              `json:"active"`
	BranchFilter string            `json:"branch_filter"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

func (h *hookJSON) toWebhook() gitoperationslib.Webhook {
	return gitoperationslib.Webhook{
		ID:           h.ID,
		URL:          h.Config["url"],
		ContentType:  gitoperationslib.HookContentType(h.Config["content_type"]),
		Events:       fromForgejoEvents(h.Events),
		Active:       h.Active,
		BranchFilter: h.BranchFilter,
		CreatedAt:    h.CreatedAt,
		UpdatedAt:    h.UpdatedAt,
	}
}

// Forgejo splits GitHub's pull_request_review into one event per verdict.
var reviewHookEvents = []string{"pull_request_review_approved", "pull_request_review_rejected", "pull_request_review_comment"}

// allHookEvents is what HookEventAll sends: Forgejo has no wildcard, and takes
// "*" as an unknown event, leaving a webhook subscribed to nothing. These are the
// names 15.0.11 accepts.
var allHookEvents = append([]string{
	"create", "delete", "fork", "push", "issues", "issue_assign", "issue_label", "issue_milestone", "issue_comment",
	"pull_request", "pull_request_assign", "pull_request_label", "pull_request_milestone", "pull_request_comment",
	"pull_request_review_request", "pull_request_sync", "wiki", "repository", "release", "package",
	"action_run_failure", "action_run_recover", "action_run_success",
}, reviewHookEvents...)

func toForgejoEvents(events []gitoperationslib.HookEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e {
		case gitoperationslib.HookEventPullRequestReview:
			out = append(out, reviewHookEvents...)
		case gitoperationslib.HookEventAll:
			out = append(out, allHookEvents...)
		default:
			out = append(out, string(e))
		}
	}
	return out
}

func fromForgejoEvents(names []string) []gitoperationslib.HookEvent {
	out := make([]gitoperationslib.HookEvent, 0, len(names))
	for _, n := range names {
		e := gitoperationslib.HookEvent(n)
		if slices.Contains(reviewHookEvents, n) {
			e = gitoperationslib.HookEventPullRequestReview
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
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
	const op = "webhooks.create"
	if opts.URL == "" || len(opts.Events) == 0 {
		return nil, errors.New("forgejo: webhooks.create: url and events are required")
	}
	if opts.InsecureSSL {
		return nil, insecureSSLNotSupported(op)
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
		Type:                "forgejo",
		Config:              map[string]string{"url": opts.URL, "content_type": string(ct)},
		Events:              toForgejoEvents(opts.Events),
		Active:              &active,
		BranchFilter:        nonEmpty(opts.BranchFilter),
		AuthorizationHeader: nonEmpty(opts.AuthorizationHeader),
	}
	if opts.Secret != "" {
		body.Config["secret"] = opts.Secret
	}
	var out hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodPost, s.base, nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return checkEvents(&hook, body.Events, out.Events, op)
}

// insecureSSLNotSupported refuses what Forgejo would drop silently: skipping the
// certificate check is its SKIP_TLS_VERIFY setting, for the whole instance.
func insecureSSLNotSupported(op string) error {
	return gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "skipping the certificate check for one webhook")
}

func (s hookService) edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	if opts.InsecureSSL != nil && *opts.InsecureSSL {
		return nil, insecureSSLNotSupported("webhooks.edit")
	}
	body := hookBody{
		Active:              opts.Active,
		BranchFilter:        opts.BranchFilter,
		AuthorizationHeader: opts.AuthorizationHeader,
	}
	config := map[string]string{}
	if opts.URL != nil {
		config["url"] = *opts.URL
	}
	if opts.ContentType != nil {
		config["content_type"] = string(*opts.ContentType)
	}
	if opts.Secret != nil {
		config["secret"] = *opts.Secret
	}
	if len(config) > 0 {
		body.Config = config
	}
	if opts.Events != nil {
		if len(opts.Events) == 0 {
			return nil, errors.New("forgejo: webhooks.edit: Events must not be empty")
		}
		body.Events = toForgejoEvents(opts.Events)
	}
	var out hookJSON
	if _, err := s.c.api.Do(ctx, http.MethodPatch, s.path(id), nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return checkEvents(&hook, body.Events, out.Events, "webhooks.edit")
}

func (s hookService) delete(ctx context.Context, id int64) error {
	_, err := s.c.api.Do(ctx, http.MethodDelete, s.path(id), nil, nil, nil)
	return err
}

// Webhooks implements gitoperationslib.Client.
func (c *Client) Webhooks() gitoperationslib.Webhooks { return webhooks{c} }

type webhooks struct{ c *Client }

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

var (
	_ gitoperationslib.UserHooks = (*Client)(nil)
	_ gitoperationslib.SiteHooks = (*Client)(nil)
)

func (c *Client) UserWebhooks() gitoperationslib.UserWebhooks {
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

func (c *Client) SystemWebhooks() gitoperationslib.SystemWebhooks {
	return systemHooks{hookService{c, "admin/hooks"}}
}

func (c *Client) DefaultWebhooks() gitoperationslib.DefaultWebhooks {
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

// Edit reads the hook first: Forgejo answers HTTP 500 to a PATCH on the ID of a
// repository's hook, and 404 to a GET.
func (h defaultHooks) Edit(ctx context.Context, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	if _, err := h.Get(ctx, id); err != nil {
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
