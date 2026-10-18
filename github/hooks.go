package github

import (
	"context"
	"errors"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// GitHub calls the wiki event "gollum".
func toGitHubEvents(events []gitoperationslib.HookEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e)
		if e == gitoperationslib.HookEventWiki {
			out[i] = "gollum"
		}
	}
	return out
}

func toWebhook(h *gh.Hook) gitoperationslib.Webhook {
	events := make([]gitoperationslib.HookEvent, len(h.Events))
	for i, e := range h.Events {
		events[i] = gitoperationslib.HookEvent(e)
		if e == "gollum" {
			events[i] = gitoperationslib.HookEventWiki
		}
	}
	return gitoperationslib.Webhook{
		ID:          h.GetID(),
		URL:         h.GetConfig().GetURL(),
		ContentType: gitoperationslib.HookContentType(h.GetConfig().GetContentType()),
		Events:      events,
		Active:      h.GetActive(),
		CreatedAt:   h.GetCreatedAt().Time,
		UpdatedAt:   h.GetUpdatedAt().Time,
	}
}

// Webhooks implements gitoperationslib.Client.
func (c *Client) Webhooks() gitoperationslib.Webhooks { return webhooks{c} }

type webhooks struct{ c *Client }

func checkScope(scope gitoperationslib.HookScope) error {
	if scope.Owner == "" {
		return errors.New("github: webhooks: scope owner is required")
	}
	return nil
}

func singleHook(h *gh.Hook, _ *gh.Response, err error) (*gitoperationslib.Webhook, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toWebhook(h)
	return &out, nil
}

func (w webhooks) List(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
	if err := checkScope(scope); err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, err
	}
	lo := &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage}
	var (
		list []*gh.Hook
		resp *gh.Response
		err  error
	)
	if scope.Repo == "" {
		list, resp, err = w.c.gh.Organizations.ListHooks(ctx, scope.Owner, lo)
	} else {
		list, resp, err = w.c.gh.Repositories.ListHooks(ctx, scope.Owner, scope.Repo, lo)
	}
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Webhook]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Webhook]{Items: make([]gitoperationslib.Webhook, len(list)), NextPage: resp.NextPage}
	for i, h := range list {
		page.Items[i] = toWebhook(h)
	}
	return page, nil
}

func (w webhooks) Get(ctx context.Context, scope gitoperationslib.HookScope, id int64) (*gitoperationslib.Webhook, error) {
	if err := checkScope(scope); err != nil {
		return nil, err
	}
	if scope.Repo == "" {
		return singleHook(w.c.gh.Organizations.GetHook(ctx, scope.Owner, id))
	}
	return singleHook(w.c.gh.Repositories.GetHook(ctx, scope.Owner, scope.Repo, id))
}

func (w webhooks) Create(ctx context.Context, scope gitoperationslib.HookScope, opts gitoperationslib.CreateWebhookOptions) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.create"
	if err := checkScope(scope); err != nil {
		return nil, err
	}
	if opts.URL == "" || len(opts.Events) == 0 {
		return nil, errors.New("github: webhooks.create: url and events are required")
	}
	if opts.BranchFilter != "" {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "branch filters")
	}
	if opts.AuthorizationHeader != "" {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "authorization headers")
	}
	ct := opts.ContentType
	if ct == "" {
		ct = gitoperationslib.HookContentJSON
	}
	active := true
	if opts.Active != nil {
		active = *opts.Active
	}
	hook := &gh.Hook{
		Name:   gh.Ptr("web"),
		Config: &gh.HookConfig{URL: &opts.URL, ContentType: gh.Ptr(string(ct)), Secret: nonEmpty(opts.Secret)},
		Events: toGitHubEvents(opts.Events),
		Active: &active,
	}
	if scope.Repo == "" {
		return singleHook(w.c.gh.Organizations.CreateHook(ctx, scope.Owner, hook))
	}
	return singleHook(w.c.gh.Repositories.CreateHook(ctx, scope.Owner, scope.Repo, hook))
}

func (w webhooks) Edit(ctx context.Context, scope gitoperationslib.HookScope, id int64, opts gitoperationslib.EditWebhookOptions) (*gitoperationslib.Webhook, error) {
	const op = "webhooks.edit"
	if err := checkScope(scope); err != nil {
		return nil, err
	}
	if opts.BranchFilter != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "branch filters")
	}
	if opts.AuthorizationHeader != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "authorization headers")
	}
	body := &gh.Hook{Active: opts.Active}
	if opts.Events != nil {
		if len(opts.Events) == 0 {
			return nil, errors.New("github: webhooks.edit: Events must not be empty")
		}
		body.Events = toGitHubEvents(opts.Events)
	}
	if opts.URL != nil || opts.ContentType != nil || opts.Secret != nil {
		// GitHub replaces the whole config: start from the current one. The
		// secret cannot be read back, so it must be passed again if set.
		cur, err := w.Get(ctx, scope, id)
		if err != nil {
			return nil, err
		}
		cfg := &gh.HookConfig{URL: gh.Ptr(cur.URL), ContentType: gh.Ptr(string(cur.ContentType)), Secret: opts.Secret}
		if opts.URL != nil {
			cfg.URL = opts.URL
		}
		if opts.ContentType != nil {
			cfg.ContentType = gh.Ptr(string(*opts.ContentType))
		}
		body.Config = cfg
	}
	if scope.Repo == "" {
		return singleHook(w.c.gh.Organizations.EditHook(ctx, scope.Owner, id, body))
	}
	return singleHook(w.c.gh.Repositories.EditHook(ctx, scope.Owner, scope.Repo, id, body))
}

func (w webhooks) Delete(ctx context.Context, scope gitoperationslib.HookScope, id int64) error {
	if err := checkScope(scope); err != nil {
		return err
	}
	var err error
	if scope.Repo == "" {
		_, err = w.c.gh.Organizations.DeleteHook(ctx, scope.Owner, id)
	} else {
		_, err = w.c.gh.Repositories.DeleteHook(ctx, scope.Owner, scope.Repo, id)
	}
	return convertError(err)
}

func (w webhooks) Ping(ctx context.Context, scope gitoperationslib.HookScope, id int64) error {
	if err := checkScope(scope); err != nil {
		return err
	}
	var err error
	if scope.Repo == "" {
		_, err = w.c.gh.Organizations.PingHook(ctx, scope.Owner, id)
	} else {
		_, err = w.c.gh.Repositories.PingHook(ctx, scope.Owner, scope.Repo, id)
	}
	return convertError(err)
}
