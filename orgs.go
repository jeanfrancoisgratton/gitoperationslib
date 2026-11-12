package gitoperationslib

import (
	"context"
	"io"
	"time"
)

// Orgs reads and manages organisations and their membership. The reads (Get,
// Info, List, ListAll, Members) work with whatever rights the token has
// (CapDirectory); the rest is Forgejo's (CapAdmin). Members join an
// organisation through its teams; there is no direct "add member".
type Orgs interface {
	Get(ctx context.Context, name string) (*Org, error)
	// Info returns an organisation with its teams, members and repositories,
	// as far as the caller can see them.
	Info(ctx context.Context, name string) (*OrgInfo, error)
	// List returns the organisations user belongs to; empty user means the
	// authenticated user (whose private memberships are included).
	List(ctx context.Context, user string, opts ListOptions) (Page[Org], error)
	// ListAll returns every organisation visible to the caller (all of them for
	// a site admin). On GitHub, NextPage is an opaque cursor rather than a page
	// number: pass it back unchanged (All does).
	ListAll(ctx context.Context, opts ListOptions) (Page[Org], error)
	// Members lists an organisation's members. Callers outside the
	// organisation see its public members only.
	Members(ctx context.Context, org string, opts ListOptions) (Page[User], error)
	// IsMember reports whether user belongs to org. A user that does not
	// exist is reported as not a member.
	IsMember(ctx context.Context, org, user string) (bool, error)
	// RemoveMember removes user from org and all its teams. Removing the last
	// owner fails.
	RemoveMember(ctx context.Context, org, user string) error

	Create(ctx context.Context, opts CreateOrgOptions) (*Org, error)
	Edit(ctx context.Context, name string, opts EditOrgOptions) (*Org, error)
	// Rename changes an organisation's name. The old name keeps redirecting
	// to the new one.
	Rename(ctx context.Context, name, newName string) error
	// Delete removes an organisation. It fails while the organisation still
	// owns repositories.
	Delete(ctx context.Context, name string) error

	// SetAvatar uploads image (PNG, JPEG, GIF…; at most MaxAvatarSize bytes)
	// as org's avatar (CapOrgAvatars). The caller must own the organisation or
	// be a site admin, and a token needs the write:organization scope. The
	// server checks the image: Forgejo answers HTTP 500 for an unknown format
	// or an image wider or taller than 4096 pixels.
	SetAvatar(ctx context.Context, org string, image io.Reader) error
	// DeleteAvatar restores org's default avatar. It succeeds when org has
	// none.
	DeleteAvatar(ctx context.Context, org string) error

	// Labels are the organisation's labels (CapOrgLabels).
	Labels() OrgLabels
}

// OrgInfo is an organisation with the teams, members and repositories the
// caller can see.
type OrgInfo struct {
	Org
	Teams   []Team `json:"teams"`
	Members []User `json:"members"` // non-members only see public members
	Repos   []Repo `json:"repos"`
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	Incomplete error       `json:"-"`
	Missing    []PartError `json:"missing,omitzero"` // see UserInfo.Missing
}

// Org is an organisation.
type Org struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	FullName    string          `json:"fullName"`
	Description string          `json:"description"`
	Website     string          `json:"website"`
	Location    string          `json:"location"`
	Email       string          `json:"email"`
	Visibility  OwnerVisibility `json:"visibility"`
	AvatarURL   string          `json:"avatarUrl"`
	HTMLURL     string          `json:"htmlUrl,omitzero"` // GitHub only
	// CreatedAt is zero on Forgejo before 16, on Bitbucket, and in GitHub's lists
	// of organisations.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// RepoAdminChangeTeamAccess lets repository admins change team access.
	RepoAdminChangeTeamAccess bool `json:"repoAdminChangeTeamAccess,omitzero"`
}

// CreateOrgOptions describes an organisation to create.
type CreateOrgOptions struct {
	// Owner is the user who will own the organisation. Empty means the
	// authenticated user; another user requires a site-admin token.
	Owner                     string          `json:"owner,omitzero"`
	Name                      string          `json:"name"` // required
	FullName                  string          `json:"fullName,omitzero"`
	Description               string          `json:"description,omitzero"`
	Website                   string          `json:"website,omitzero"`
	Location                  string          `json:"location,omitzero"`
	Email                     string          `json:"email,omitzero"`
	Visibility                OwnerVisibility `json:"visibility,omitzero"` // zero value: backend default
	RepoAdminChangeTeamAccess bool            `json:"repoAdminChangeTeamAccess,omitzero"`
}

// EditOrgOptions changes an organisation. Nil fields are left unchanged.
type EditOrgOptions struct {
	FullName                  *string          `json:"fullName,omitzero"`
	Description               *string          `json:"description,omitzero"`
	Website                   *string          `json:"website,omitzero"`
	Location                  *string          `json:"location,omitzero"`
	Email                     *string          `json:"email,omitzero"`
	Visibility                *OwnerVisibility `json:"visibility,omitzero"`
	RepoAdminChangeTeamAccess *bool            `json:"repoAdminChangeTeamAccess,omitzero"`
}
