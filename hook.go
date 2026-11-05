package gitoperationslib

import (
	"context"
	"time"
)

// UserHooks gives access to the authenticated user's own webhooks. Only the
// Forgejo backend implements it (CapUserWebhooks); find it with a type
// assertion:
//
//	h, ok := client.(gitoperationslib.UserHooks)
type UserHooks interface {
	UserWebhooks() UserWebhooks
}

// UserWebhooks manages the webhooks of the authenticated user, which fire for
// events in that user's repositories. They are always the caller's own: a site
// admin gets a 404 for another user's hook.
type UserWebhooks interface {
	List(ctx context.Context, opts ListOptions) (Page[Webhook], error)
	Get(ctx context.Context, id int64) (*Webhook, error)
	// Create adds a webhook. Forgejo silently drops event names it does not
	// know; Create then returns the webhook together with an error.
	Create(ctx context.Context, opts CreateWebhookOptions) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in Create.
	Edit(ctx context.Context, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, id int64) error
}

// SiteHooks gives access to a Forgejo instance's system and default webhooks,
// which need a site-admin token (HTTP 403 otherwise). Only the Forgejo backend
// implements it (CapSystemWebhooks); find it with a type assertion:
//
//	h, ok := client.(gitoperationslib.SiteHooks)
//
// Forgejo's API addresses both kinds by ID at the same path, and nothing in its
// answers tells them apart. Both interfaces therefore check the kind by listing
// the system webhooks first, and answer an ID of the other kind with a 404
// *APIError, changing nothing.
type SiteHooks interface {
	SystemWebhooks() SystemWebhooks
	DefaultWebhooks() DefaultWebhooks
}

// SystemWebhooks manages system webhooks, which fire for events in every
// repository of the instance. There is no Create: Forgejo's API cannot create
// one, so they are created in the web interface (Site administration, Webhooks).
type SystemWebhooks interface {
	List(ctx context.Context, opts ListOptions) (Page[Webhook], error)
	Get(ctx context.Context, id int64) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in
	// Webhooks.Create.
	Edit(ctx context.Context, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, id int64) error
}

// DefaultWebhooks manages default webhooks: templates copied into each
// repository created afterwards. Existing repositories are not affected, and
// the copies are independent repository webhooks: editing or deleting the
// default webhook leaves them as they are. There is no List: Forgejo's API
// cannot list them, so keep the ID that Create returns.
type DefaultWebhooks interface {
	// Create adds a default webhook. Forgejo silently drops event names it does
	// not know; Create then returns the webhook together with an error.
	Create(ctx context.Context, opts CreateWebhookOptions) (*Webhook, error)
	Get(ctx context.Context, id int64) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in Create.
	Edit(ctx context.Context, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, id int64) error
}

// HookScope selects where webhooks live: a repository, or an organisation
// when Repo is empty.
type HookScope struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo,omitzero"`
}

// RepoHooks is the scope of a repository's webhooks.
func RepoHooks(owner, repo string) HookScope { return HookScope{Owner: owner, Repo: repo} }

// OrgHooks is the scope of an organisation's webhooks.
func OrgHooks(org string) HookScope { return HookScope{Owner: org} }

// HookEvent is an event a webhook subscribes to. The constants use GitHub's
// names and are translated for Forgejo; other values are passed to the
// server unchanged, for backend-specific events.
type HookEvent string

const (
	HookEventPush              HookEvent = "push"
	HookEventCreate            HookEvent = "create" // branch or tag created
	HookEventDelete            HookEvent = "delete" // branch or tag deleted
	HookEventFork              HookEvent = "fork"
	HookEventRelease           HookEvent = "release"
	HookEventIssues            HookEvent = "issues"
	HookEventIssueComment      HookEvent = "issue_comment"
	HookEventPullRequest       HookEvent = "pull_request"
	HookEventPullRequestReview HookEvent = "pull_request_review"
	HookEventRepository        HookEvent = "repository"
	HookEventWiki              HookEvent = "wiki" // GitHub: gollum
)

// HookContentType is the encoding of webhook payloads.
type HookContentType string

const (
	HookContentJSON HookContentType = "json"
	HookContentForm HookContentType = "form" // payload=<json>, URL-encoded
)

// Webhook is a configured webhook. The secret is never returned.
type Webhook struct {
	ID          int64           `json:"id"`
	URL         string          `json:"url"`
	ContentType HookContentType `json:"contentType"`
	// Events lists the subscribed events. Forgejo may add finer-grained
	// events implied by the requested ones (e.g. issue_assign with issues);
	// those keep Forgejo's names.
	Events       []HookEvent `json:"events"`
	Active       bool        `json:"active"`
	BranchFilter string      `json:"branchFilter,omitzero"` // Forgejo only
	CreatedAt    time.Time   `json:"createdAt,omitzero"`
	UpdatedAt    time.Time   `json:"updatedAt,omitzero"`
}

// CreateWebhookOptions describes a webhook to create.
type CreateWebhookOptions struct {
	URL         string          `json:"url"`                  // required
	ContentType HookContentType `json:"contentType,omitzero"` // zero value: json
	// Secret signs deliveries (see the webhook package). Strongly
	// recommended.
	Secret string      `json:"-"`
	Events []HookEvent `json:"events"`          // required
	Active *bool       `json:"active,omitzero"` // nil: active
	// BranchFilter limits push and branch events to matching branches, as a
	// glob such as "{main,release/*}" (Forgejo only).
	BranchFilter string `json:"branchFilter,omitzero"`
	// AuthorizationHeader is sent with every delivery (Forgejo only).
	AuthorizationHeader string `json:"-"`
}

// EditWebhookOptions changes a webhook. Nil fields are left unchanged; a
// non-nil Events replaces the subscription list.
type EditWebhookOptions struct {
	URL                 *string          `json:"url,omitzero"`
	ContentType         *HookContentType `json:"contentType,omitzero"`
	Secret              *string          `json:"-"`
	Events              []HookEvent      `json:"events,omitzero"`
	Active              *bool            `json:"active,omitzero"`
	BranchFilter        *string          `json:"branchFilter,omitzero"` // Forgejo only
	AuthorizationHeader *string          `json:"-"`                     // Forgejo only
}
