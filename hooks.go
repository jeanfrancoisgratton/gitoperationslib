package gitoperationslib

import (
	"context"
	"encoding/json"
	"time"
)

// Webhooks manages repository and organisation webhooks. Receiving and
// verifying deliveries is done by the webhook sub-package. The delivery
// history is GitHub's only (CapHookDeliveries): the APIs of Forgejo and
// Bitbucket have none.
type Webhooks interface {
	List(ctx context.Context, scope HookScope, opts ListOptions) (Page[Webhook], error)
	Get(ctx context.Context, scope HookScope, id int64) (*Webhook, error)
	// Create adds a webhook. Forgejo silently drops event names it does not
	// know; Create then returns the webhook together with an error.
	Create(ctx context.Context, scope HookScope, opts CreateWebhookOptions) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in Create.
	Edit(ctx context.Context, scope HookScope, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, scope HookScope, id int64) error
	// Ping asks the server to send a test delivery: a "ping" event on
	// GitHub, a "push" of the latest commit on Forgejo (repositories only).
	Ping(ctx context.Context, scope HookScope, id int64) error

	// ListDeliveries lists deliveries, newest first, without the request
	// and response. GitHub keeps them for a few days.
	ListDeliveries(ctx context.Context, scope HookScope, hookID int64, opts ListHookDeliveriesOptions) (HookDeliveryPage, error)
	// GetDelivery returns a delivery with its request and response.
	GetDelivery(ctx context.Context, scope HookScope, hookID, id int64) (*HookDelivery, error)
	// Redeliver queues a delivery to be sent again, and returns at once.
	// It shows up as a new delivery, with Redelivery set.
	Redeliver(ctx context.Context, scope HookScope, hookID, id int64) error
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
	// HookEventWorkflowRun and HookEventWorkflowJob follow CI workflows: GitHub,
	// and Forgejo from 17 on, where each stands for its seven per-status events
	// (ErrNotSupported on older servers). Subscribe to fewer with Forgejo's
	// names, such as "workflow_run_failure". Bitbucket has no equivalent.
	HookEventWorkflowRun HookEvent = "workflow_run"
	HookEventWorkflowJob HookEvent = "workflow_job"
	// HookEventAll subscribes to everything. Only GitHub has a wildcard, which
	// also covers events added later, and reports it back as "*". For the
	// others the library sends the events it knows, which come back one by one.
	// On Forgejo that means workflow_* from 17 on, and the older action_run_*
	// below it.
	HookEventAll HookEvent = "*"
)

// HookType is the format of a webhook's deliveries (Forgejo only).
type HookType string

const (
	HookTypeForgejo    HookType = "forgejo"
	HookTypeGitea      HookType = "gitea"
	HookTypeGogs       HookType = "gogs"
	HookTypeSlack      HookType = "slack" // also Mattermost; needs Slack settings
	HookTypeDiscord    HookType = "discord"
	HookTypeDingTalk   HookType = "dingtalk"
	HookTypeMSTeams    HookType = "msteams"
	HookTypeFeishu     HookType = "feishu"
	HookTypeWeChatWork HookType = "wechatwork"
	// These can be read but not created: Forgejo's API drops the settings they
	// need, leaving a webhook that cannot deliver.
	HookTypeTelegram        HookType = "telegram"
	HookTypeMatrix          HookType = "matrix"
	HookTypePackagist       HookType = "packagist"
	HookTypeSourceHutBuilds HookType = "sourcehut_builds"
)

// SlackHookSettings holds what a HookTypeSlack webhook needs.
type SlackHookSettings struct {
	Channel  string `json:"channel"` // required
	Username string `json:"username,omitzero"`
	IconURL  string `json:"iconUrl,omitzero"`
	Color    string `json:"color,omitzero"`
}

// HookContentType is the encoding of webhook payloads.
type HookContentType string

const (
	HookContentJSON HookContentType = "json"
	HookContentForm HookContentType = "form" // payload=<json>, URL-encoded
)

// Webhook is a configured webhook. The secret is never returned.
type Webhook struct {
	ID          int64           `json:"id"`
	Type        HookType        `json:"type,omitzero"` // Forgejo only
	URL         string          `json:"url"`
	ContentType HookContentType `json:"contentType"`
	// Events lists the subscribed events. Forgejo may add finer-grained
	// events implied by the requested ones (e.g. issue_assign with issues);
	// those keep Forgejo's names.
	Events       []HookEvent `json:"events"`
	Active       bool        `json:"active"`
	BranchFilter string      `json:"branchFilter,omitzero"` // Forgejo only
	// HasAuthorizationHeader tells whether deliveries carry one. Its value is
	// left out on purpose: it is a credential (Forgejo only).
	HasAuthorizationHeader bool               `json:"hasAuthorizationHeader,omitzero"`
	Slack                  *SlackHookSettings `json:"slack,omitzero"` // HookTypeSlack only
	// InsecureSSL delivers without checking the target's TLS certificate
	// (GitHub, Bitbucket).
	InsecureSSL bool      `json:"insecureSsl,omitzero"`
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
}

// CreateWebhookOptions describes a webhook to create.
type CreateWebhookOptions struct {
	// Type is the format of the deliveries (Forgejo only). Zero value:
	// HookTypeForgejo.
	Type        HookType        `json:"type,omitzero"`
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
	// Slack is required with HookTypeSlack, and refused with any other type.
	Slack *SlackHookSettings `json:"slack,omitzero"`
	// InsecureSSL delivers without checking the target's TLS certificate, so a
	// third party could read or alter deliveries (GitHub, Bitbucket; on Forgejo
	// it is an instance setting, and true returns ErrNotSupported).
	InsecureSSL bool `json:"insecureSsl,omitzero"`
}

// EditWebhookOptions changes a webhook. Nil fields are left unchanged; a
// non-nil Events replaces the subscription list. On Forgejo, its API cannot
// change the package, action_run_*, workflow_run_* and workflow_job_* events of
// an existing webhook: Events must keep those as they are, or Edit returns
// ErrNotSupported and changes nothing. To change them, create a new webhook and
// delete this one.
//
// On Forgejo, Edit reads the webhook first, as a PATCH without events and a
// branch filter resets them to "push" and none. It is not atomic: a change made
// between the two requests is overwritten.
type EditWebhookOptions struct {
	URL         *string          `json:"url,omitzero"`
	ContentType *HookContentType `json:"contentType,omitzero"`
	// Secret returns ErrNotSupported on Forgejo, whose API ignores a new secret
	// and keeps signing with the old one. To rotate it there, create a new
	// webhook and delete this one.
	Secret       *string     `json:"-"`
	Events       []HookEvent `json:"events,omitzero"`
	Active       *bool       `json:"active,omitzero"`
	BranchFilter *string     `json:"branchFilter,omitzero"` // Forgejo only
	// AuthorizationHeader replaces the header (Forgejo only). Forgejo's API
	// cannot remove it, so an empty value returns ErrNotSupported.
	AuthorizationHeader *string `json:"-"`
	// Slack replaces all the settings of a HookTypeSlack webhook.
	Slack       *SlackHookSettings `json:"slack,omitzero"`
	InsecureSSL *bool              `json:"insecureSsl,omitzero"` // see CreateWebhookOptions
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
