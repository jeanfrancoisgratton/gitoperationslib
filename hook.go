package gitoperationslib

import "time"

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
