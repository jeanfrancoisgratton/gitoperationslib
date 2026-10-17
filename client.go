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
