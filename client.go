package gitoperationslib

import "context"

// Client is the entry point to a forge backend.
//
// Areas a backend may not support are exposed through optional interfaces;
// check Capabilities, or use a type assertion, before relying on them.
type Client interface {
	Backend() Backend
	Capabilities() Capability
	Repos() Repos
}

// Admin manages accounts. Only the Forgejo backend implements it; detect it
// with a type assertion (client.(gitoperationslib.Admin)) or CapAdmin.
type Admin interface {
	Users() Users
	Orgs() Orgs
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
