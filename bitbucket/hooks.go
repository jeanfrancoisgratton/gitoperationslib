package bitbucket

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
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

// Bitbucket event keys for each HookEvent. Events missing here (create,
// delete, release, issues, issue_comment, wiki) have no Bitbucket
// equivalent; keys containing ":" are passed through unchanged.
var hookEventKeys = map[gitoperationslib.HookEvent][]string{
	gitoperationslib.HookEventPush:              {"repo:push"},
	gitoperationslib.HookEventFork:              {"repo:fork"},
	gitoperationslib.HookEventRepository:        {"repo:updated"},
	gitoperationslib.HookEventPullRequest:       {"pullrequest:created", "pullrequest:updated", "pullrequest:fulfilled", "pullrequest:rejected"},
	gitoperationslib.HookEventPullRequestReview: {"pullrequest:approved", "pullrequest:unapproved", "pullrequest:changes_request_created", "pullrequest:changes_request_removed"},
}

func toEventKeys(events []gitoperationslib.HookEvent, op string) ([]string, error) {
	var keys []string
	for _, e := range events {
		switch k, ok := hookEventKeys[e]; {
		case ok:
			keys = append(keys, k...)
		case strings.Contains(string(e), ":"):
			keys = append(keys, string(e))
		default:
			return nil, notSupported(op, fmt.Sprintf("event %q", e))
		}
	}
	return keys, nil
}

// fromEventKeys folds complete groups of keys back into HookEvents; other
// keys are reported as they are.
func fromEventKeys(keys []string) []gitoperationslib.HookEvent {
	used := map[string]bool{}
	var events []gitoperationslib.HookEvent
	for _, e := range []gitoperationslib.HookEvent{
		gitoperationslib.HookEventPush, gitoperationslib.HookEventFork, gitoperationslib.HookEventRepository,
		gitoperationslib.HookEventPullRequest, gitoperationslib.HookEventPullRequestReview,
	} {
		group := hookEventKeys[e]
		if !slices.ContainsFunc(group, func(k string) bool { return !slices.Contains(keys, k) }) {
			events = append(events, e)
			for _, k := range group {
				used[k] = true
			}
		}
	}
	for _, k := range keys {
		if !used[k] {
			events = append(events, gitoperationslib.HookEvent(k))
		}
	}
	return events
}

// hookID derives a stable numeric ID from a webhook's UUID (Bitbucket has no
// numeric IDs): the first 15 hex digits.
func hookID(uuid string) int64 {
	hex := strings.NewReplacer("{", "", "}", "", "-", "").Replace(uuid)
	if len(hex) > 15 {
		hex = hex[:15]
	}
	n, _ := strconv.ParseInt(hex, 16, 64)
	return n
}

type hookJSON struct {
	UUID        string    `json:"uuid,omitempty"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	Events      []string  `json:"events"`
	Secret      *string   `json:"secret,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
}

func (h *hookJSON) toWebhook() gitoperationslib.Webhook {
	return gitoperationslib.Webhook{
		ID: hookID(h.UUID), URL: h.URL, ContentType: gitoperationslib.HookContentJSON,
		Events: fromEventKeys(h.Events), Active: h.Active, CreatedAt: h.CreatedAt, UpdatedAt: h.CreatedAt,
	}
}

// Webhooks implements gitoperationslib.Client. Webhook IDs are derived from
// Bitbucket's UUIDs. Hook URLs must resolve in DNS.
func (c *Client) Webhooks() gitoperationslib.Webhooks { return webhooks{c} }

type webhooks struct{ c *Client }

func hooksPath(scope gitoperationslib.HookScope) (string, error) {
	switch {
	case scope.Owner == "":
		return "", errors.New("bitbucket: webhooks: scope owner (workspace) is required")
	case scope.Repo == "":
		return bitbucketapi.Path("workspaces", scope.Owner, "hooks"), nil
	}
	return repoPath(scope.Owner, scope.Repo, "hooks"), nil
}

func (w webhooks) find(ctx context.Context, scope gitoperationslib.HookScope, id int64) (*hookJSON, string, error) {
	base, err := hooksPath(scope)
	if err != nil {
		return nil, "", err
	}
	all, err := bitbucketapi.All[hookJSON](ctx, w.c.api, base, nil)
	if err != nil {
		return nil, "", err
	}
	for i := range all {
		if hookID(all[i].UUID) == id {
			return &all[i], base + "/" + bitbucketapi.Path(all[i].UUID), nil
		}
	}
	return nil, "", &gitoperationslib.APIError{
		Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("no webhook %d", id),
	}
}

func (w webhooks) List(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	base, err := hooksPath(scope)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, err
	}
	page, err := bitbucketapi.List[hookJSON](ctx, w.c.api, base, bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Webhook]{Items: make([]gitoperationslib.Webhook, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toWebhook()
	}
	return out, nil
}

func (w webhooks) Get(ctx context.Context, scope gitoperationslib.HookScope, id int64) (*gitoperationslib.Webhook, error) {
	h, _, err := w.find(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	hook := h.toWebhook()
	return &hook, nil
}

func (w webhooks) Create(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.create"
	base, err := hooksPath(scope)
	if err != nil {
		return nil, err
	}
	switch {
	case opts.URL == "" || len(opts.Events) == 0:
		return nil, errors.New("bitbucket: webhooks.create: url and events are required")
	case opts.ContentType == gitoperationslib.HookContentForm:
		return nil, notSupported(op, "form-encoded payloads")
	case opts.BranchFilter != "":
		return nil, notSupported(op, "branch filters")
	case opts.AuthorizationHeader != "":
		return nil, notSupported(op, "authorization headers")
	}
	keys, err := toEventKeys(opts.Events, op)
	if err != nil {
		return nil, err
	}
	body := hookJSON{URL: opts.URL, Description: "gitoperationslib", Active: opts.Active == nil || *opts.Active, Events: keys}
	if opts.Secret != "" {
		body.Secret = &opts.Secret
	}
	var out hookJSON
	if _, err := w.c.api.Do(ctx, http.MethodPost, base, nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return &hook, nil
}

func (w webhooks) Edit(ctx context.Context, scope gitoperationslib.HookScope, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.edit"
	switch {
	case opts.ContentType != nil && *opts.ContentType == gitoperationslib.HookContentForm:
		return nil, notSupported(op, "form-encoded payloads")
	case opts.BranchFilter != nil:
		return nil, notSupported(op, "branch filters")
	case opts.AuthorizationHeader != nil:
		return nil, notSupported(op, "authorization headers")
	}
	cur, path, err := w.find(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	body := hookJSON{URL: cur.URL, Description: cur.Description, Active: cur.Active, Events: cur.Events, Secret: opts.Secret}
	if opts.URL != nil {
		body.URL = *opts.URL
	}
	if opts.Active != nil {
		body.Active = *opts.Active
	}
	if opts.Events != nil {
		if len(opts.Events) == 0 {
			return nil, errors.New("bitbucket: webhooks.edit: Events must not be empty")
		}
		if body.Events, err = toEventKeys(opts.Events, op); err != nil {
			return nil, err
		}
	}
	var out hookJSON
	if _, err := w.c.api.Do(ctx, http.MethodPut, path, nil, body, &out); err != nil {
		return nil, err
	}
	hook := out.toWebhook()
	return &hook, nil
}

func (w webhooks) Delete(ctx context.Context, scope gitoperationslib.HookScope, id int64) error {
	_, path, err := w.find(ctx, scope, id)
	if err != nil {
		return err
	}
	_, err = w.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

func (w webhooks) Ping(context.Context, gitoperationslib.HookScope, int64) error {
	return notSupported("webhooks.ping", "test deliveries")
}
