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
