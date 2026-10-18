package gitoperationslib

import (
	"context"
	"io"
)

// Client is the entry point to a forge backend.
//
// Areas a backend may not support are exposed through optional interfaces;
// check Capabilities, or use a type assertion, before relying on them.
type Client interface {
	Backend() Backend
	Capabilities() Capability
	Repos() Repos
	Releases() Releases
	PullRequests() PullRequests
	Webhooks() Webhooks
}

// Webhooks manages repository and organisation webhooks. Receiving and
// verifying deliveries is done by the webhook sub-package.
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
}

// PullRequests manages pull requests and their reviews.
type PullRequests interface {
	List(ctx context.Context, owner, repo string, opts ListPROptions) (Page[PullRequest], error)
	Get(ctx context.Context, owner, repo string, number int) (*PullRequest, error)
	// Create opens a pull request. If the server does not honour Draft
	// (Forgejo with custom work-in-progress prefixes), the pull request is
	// returned together with an error matching ErrNotSupported.
	Create(ctx context.Context, owner, repo string, opts CreatePROptions) (*PullRequest, error)
	// Edit changes a pull request; see EditPROptions. A Draft change the
	// server does not honour is reported as in Create.
	Edit(ctx context.Context, owner, repo string, number int, opts EditPROptions) (*PullRequest, error)
	// Merge merges a pull request. Drafts cannot be merged.
	Merge(ctx context.Context, owner, repo string, number int, opts MergeOptions) error
	IsMerged(ctx context.Context, owner, repo string, number int) (bool, error)

	RequestReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error
	RemoveReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error
	ListReviews(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[Review], error)
	CreateReview(ctx context.Context, owner, repo string, number int, opts CreateReviewOptions) (*Review, error)
}

// Releases manages releases and their assets.
//
// Asset methods take the release ID as well as the asset ID: Forgejo needs
// both, GitHub ignores the release ID.
type Releases interface {
	List(ctx context.Context, owner, repo string, opts ListOptions) (Page[Release], error)
	Get(ctx context.Context, owner, repo string, id int64) (*Release, error)
	GetByTag(ctx context.Context, owner, repo, tag string) (*Release, error)
	// Latest returns the most recent published release that is not a
	// prerelease.
	Latest(ctx context.Context, owner, repo string) (*Release, error)
	Create(ctx context.Context, owner, repo string, opts CreateReleaseOptions) (*Release, error)
	Edit(ctx context.Context, owner, repo string, id int64, opts EditReleaseOptions) (*Release, error)
	// Delete removes a release. Its tag is kept.
	Delete(ctx context.Context, owner, repo string, id int64) error

	ListAssets(ctx context.Context, owner, repo string, releaseID int64, opts ListOptions) (Page[Asset], error)
	GetAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) (*Asset, error)
	// UploadAsset streams size bytes from r as a new asset called name.
	// size must be exact. An empty contentType is guessed from the name's
	// extension, falling back to application/octet-stream.
	UploadAsset(ctx context.Context, owner, repo string, releaseID int64, name string, r io.Reader, size int64, contentType string) (*Asset, error)
	EditAsset(ctx context.Context, owner, repo string, releaseID, assetID int64, opts EditAssetOptions) (*Asset, error)
	DeleteAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) error
	// DownloadAsset streams an uploaded asset's content; the caller must
	// close it. External assets are not downloaded: fetch their DownloadURL.
	DownloadAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) (io.ReadCloser, error)
}

// ExternalAssets adds release assets that link to files hosted elsewhere.
// Only the Forgejo backend implements it (CapExternalAssets), on the value
// returned by Releases():
//
//	ext, ok := client.Releases().(gitoperationslib.ExternalAssets)
type ExternalAssets interface {
	AddExternalAsset(ctx context.Context, owner, repo string, releaseID int64, name, url string) (*Asset, error)
}

// Admin manages accounts. Only the Forgejo backend implements it; detect it
// with a type assertion (client.(gitoperationslib.Admin)) or CapAdmin.
type Admin interface {
	Users() Users
	Orgs() Orgs
	Teams() Teams
}

// Users manages user accounts. All methods except Get require a site-admin
// token.
type Users interface {
	Get(ctx context.Context, login string) (*User, error)
	List(ctx context.Context, opts ListOptions) (Page[User], error)
	Create(ctx context.Context, opts CreateUserOptions) (*User, error)
	// Edit applies opts. If the server ignores a visibility change (Forgejo
	// 15.0.x cannot switch a user back to public), Edit returns the updated
	// user together with an error matching ErrNotSupported.
	Edit(ctx context.Context, login string, opts EditUserOptions) (*User, error)
	// Rename changes a user's login. The old login keeps redirecting to the
	// new one, so Get(oldLogin) returns the renamed user.
	Rename(ctx context.Context, login, newLogin string) error
	Delete(ctx context.Context, login string, opts DeleteUserOptions) error
}

// Orgs manages organisations and their membership. Members join an
// organisation through its teams; there is no direct "add member".
type Orgs interface {
	Get(ctx context.Context, name string) (*Org, error)
	// List returns the organisations user belongs to. Empty user means the
	// authenticated user.
	List(ctx context.Context, user string, opts ListOptions) (Page[Org], error)
	// ListAll returns every organisation visible to the authenticated user
	// (all of them for a site admin).
	ListAll(ctx context.Context, opts ListOptions) (Page[Org], error)
	Create(ctx context.Context, opts CreateOrgOptions) (*Org, error)
	Edit(ctx context.Context, name string, opts EditOrgOptions) (*Org, error)
	// Rename changes an organisation's name. The old name keeps redirecting
	// to the new one.
	Rename(ctx context.Context, name, newName string) error
	// Delete removes an organisation. It fails while the organisation still
	// owns repositories.
	Delete(ctx context.Context, name string) error

	Members(ctx context.Context, org string, opts ListOptions) (Page[User], error)
	// IsMember reports whether user belongs to org. A user that does not
	// exist is reported as not a member.
	IsMember(ctx context.Context, org, user string) (bool, error)
	// RemoveMember removes user from org and all its teams. Removing the last
	// owner fails.
	RemoveMember(ctx context.Context, org, user string) error
}

// Teams manages an organisation's teams, their members and repositories.
// Teams are addressed by ID; use GetByName to look one up.
type Teams interface {
	List(ctx context.Context, org string, opts ListOptions) (Page[Team], error)
	Get(ctx context.Context, id int64) (*Team, error)
	// GetByName returns the team of org with exactly that name
	// (case-insensitive), or a 404 *APIError.
	GetByName(ctx context.Context, org, name string) (*Team, error)
	Create(ctx context.Context, org string, opts CreateTeamOptions) (*Team, error)
	// Edit changes a team. Renaming the owner team or changing its access
	// returns ErrOwnerTeam.
	Edit(ctx context.Context, id int64, opts EditTeamOptions) (*Team, error)
	// Delete removes a team. Deleting the owner team returns ErrOwnerTeam.
	Delete(ctx context.Context, id int64) error

	Members(ctx context.Context, id int64, opts ListOptions) (Page[User], error)
	IsMember(ctx context.Context, id int64, user string) (bool, error)
	// AddMember adds user to the team, which also makes them a member of
	// the organisation.
	AddMember(ctx context.Context, id int64, user string) error
	RemoveMember(ctx context.Context, id int64, user string) error

	Repos(ctx context.Context, id int64, opts ListOptions) (Page[Repo], error)
	AddRepo(ctx context.Context, id int64, owner, repo string) error
	RemoveRepo(ctx context.Context, id int64, owner, repo string) error
}

// Repos manages repositories.
type Repos interface {
	Get(ctx context.Context, owner, name string) (*Repo, error)
	// List returns repositories owned by owner (a user or organisation).
	// Empty owner lists every repository the authenticated user can access,
	// including those of organisations they belong to.
	List(ctx context.Context, owner string, opts ListOptions) (Page[Repo], error)
	Create(ctx context.Context, opts CreateRepoOptions) (*Repo, error)
	Edit(ctx context.Context, owner, name string, opts EditRepoOptions) (*Repo, error)
	Delete(ctx context.Context, owner, name string) error
}
