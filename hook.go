package gitoperationslib

import (
	"context"
	"encoding/json"
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
// repository of the instance. The library has no Create, as Forgejo only offers
// one through an undocumented flag: create them in the web interface (Site
// administration, Webhooks).
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
	// HookEventAll subscribes to everything. Only GitHub has a wildcard, which
	// also covers events added later, and reports it back as "*". For the
	// others the library sends the events it knows, which come back one by one.
	HookEventAll HookEvent = "*"
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
	// InsecureSSL delivers without checking the target's TLS certificate
	// (GitHub, Bitbucket).
	InsecureSSL bool      `json:"insecureSsl,omitzero"`
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
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
	// InsecureSSL delivers without checking the target's TLS certificate, so a
	// third party could read or alter deliveries (GitHub, Bitbucket; on Forgejo
	// it is an instance setting, and true returns ErrNotSupported).
	InsecureSSL bool `json:"insecureSsl,omitzero"`
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
	InsecureSSL         *bool            `json:"insecureSsl,omitzero"`  // see CreateWebhookOptions
}

// HookDeliveries reads a webhook's delivery history and sends deliveries again.
// Only the GitHub backend implements it (CapHookDeliveries): the APIs of Forgejo
// and Bitbucket have no such history. Find it with a type assertion:
//
//	d, ok := client.(gitoperationslib.HookDeliveries)
type HookDeliveries interface {
	// ListHookDeliveries lists deliveries, newest first, without the request
	// and response. GitHub keeps them for a few days.
	ListHookDeliveries(ctx context.Context, scope HookScope, hookID int64, opts ListHookDeliveriesOptions) (HookDeliveryPage, error)
	// GetHookDelivery returns a delivery with its request and response.
	GetHookDelivery(ctx context.Context, scope HookScope, hookID, id int64) (*HookDelivery, error)
	// RedeliverHook queues a delivery to be sent again, and returns at once.
	// It shows up as a new delivery, with Redelivery set.
	RedeliverHook(ctx context.Context, scope HookScope, hookID, id int64) error
}

// ListHookDeliveriesOptions selects a page of deliveries.
type ListHookDeliveriesOptions struct {
	// Cursor is HookDeliveryPage.NextCursor of the previous page: this list has
	// no page numbers, so gitoperationslib.All does not apply.
	Cursor  string `json:"cursor,omitzero"`
	PerPage int    `json:"perPage,omitzero"`
}

// HookDeliveryPage is a page of deliveries.
type HookDeliveryPage struct {
	Items []HookDelivery `json:"items"`
	// NextCursor is the cursor of the next page, or empty on the last one.
	NextCursor string `json:"nextCursor"`
}

// HookDelivery is one attempt to deliver an event to a webhook.
type HookDelivery struct {
	ID int64 `json:"id"`
	// GUID is the delivery's X-GitHub-Delivery, shared by its redeliveries.
	GUID        string        `json:"guid"`
	Event       string        `json:"event"`
	Action      string        `json:"action,omitzero"`
	Status      string        `json:"status"`     // "OK", or what went wrong
	StatusCode  int           `json:"statusCode"` // the target's answer; 502 if GitHub cannot reach it
	Redelivery  bool          `json:"redelivery"`
	Duration    time.Duration `json:"duration,omitzero"`
	DeliveredAt time.Time     `json:"deliveredAt,omitzero"`
	// The rest is set by GetHookDelivery only. To decode RequestPayload with the
	// webhook package, compact it first: that gives the bytes GitHub signed.
	RequestHeaders  map[string]string `json:"requestHeaders,omitzero"`
	RequestPayload  json.RawMessage   `json:"requestPayload,omitzero"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitzero"`
	ResponseBody    string            `json:"responseBody,omitzero"`
}
