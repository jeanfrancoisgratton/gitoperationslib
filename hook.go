package gitoperationslib

import "time"

// HookScope selects where webhooks live: a repository, or an organisation
// when Repo is empty.
type HookScope struct {
	Owner string
	Repo  string
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
	ID          int64
	URL         string
	ContentType HookContentType
	// Events lists the subscribed events. Forgejo may add finer-grained
	// events implied by the requested ones (e.g. issue_assign with issues);
	// those keep Forgejo's names.
	Events       []HookEvent
	Active       bool
	BranchFilter string // Forgejo only
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateWebhookOptions describes a webhook to create.
type CreateWebhookOptions struct {
	URL         string          // required
	ContentType HookContentType // zero value: json
	// Secret signs deliveries (see the webhook package). Strongly
	// recommended.
	Secret string
	Events []HookEvent // required
	Active *bool       // nil: active
	// BranchFilter limits push and branch events to matching branches, as a
	// glob such as "{main,release/*}" (Forgejo only).
	BranchFilter string
	// AuthorizationHeader is sent with every delivery (Forgejo only).
	AuthorizationHeader string
}

// EditWebhookOptions changes a webhook. Nil fields are left unchanged; a
// non-nil Events replaces the subscription list.
type EditWebhookOptions struct {
	URL                 *string
	ContentType         *HookContentType
	Secret              *string
	Events              []HookEvent
	Active              *bool
	BranchFilter        *string // Forgejo only
	AuthorizationHeader *string // Forgejo only
}
