package gitoperationslib

import (
	"context"
	"time"
)

// CIScope selects where CI secrets and variables live: a repository, an
// organisation or workspace (Repo empty), or the authenticated user (both
// empty).
type CIScope struct {
	Owner string `json:"owner,omitzero"`
	Repo  string `json:"repo,omitzero"`
}

// RepoCI is the scope of a repository's secrets and variables.
func RepoCI(owner, repo string) CIScope { return CIScope{Owner: owner, Repo: repo} }

// OrgCI is the scope of an organisation's (Bitbucket: workspace's) secrets
// and variables, available to all its repositories.
func OrgCI(org string) CIScope { return CIScope{Owner: org} }

// UserCI is the scope of the authenticated user's own secrets and variables
// (Forgejo only).
func UserCI() CIScope { return CIScope{} }

// CI manages the secrets and variables of CI workflows: Forgejo Actions,
// GitHub Actions, Bitbucket Pipelines (CapSecrets). Find it with a type
// assertion:
//
//	ci, ok := client.(gitoperationslib.CI)
type CI interface {
	Secrets() Secrets
	Variables() Variables
}

// Secrets manages CI secrets. Their values are write-only: once stored they
// can be replaced or deleted, never read back.
//
// Forgejo and GitHub upper-case names; Bitbucket keeps them as given. Names
// are letters, digits and underscores, not starting with a digit (nor, on
// Forgejo, with GITEA_, GITHUB_ or FORGEJO_). Empty values are refused.
// Forgejo turns line endings into LF: base64-encode values whose exact bytes
// matter.
type Secrets interface {
	// List returns the secrets' names. Forgejo cannot list the user's
	// secrets (ErrNotSupported).
	List(ctx context.Context, scope CIScope, opts ListOptions) (Page[Secret], error)
	// Set creates the secret or replaces its value. On GitHub the value is
	// encrypted for the scope's public key before it is sent; organisation
	// secrets are made available to all the organisation's repositories.
	Set(ctx context.Context, scope CIScope, name, value string) error
	Delete(ctx context.Context, scope CIScope, name string) error
}

// Secret is a CI secret; its value is never returned.
type Secret struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt,omitzero"` // Forgejo and GitHub
	UpdatedAt time.Time `json:"updatedAt,omitzero"` // GitHub only
}

// Variables manages CI variables: like secrets, but readable.
type Variables interface {
	List(ctx context.Context, scope CIScope, opts ListOptions) (Page[Variable], error)
	Get(ctx context.Context, scope CIScope, name string) (*Variable, error)
	// Set creates the variable or changes its value.
	Set(ctx context.Context, scope CIScope, name, value string) error
	Delete(ctx context.Context, scope CIScope, name string) error
}

// Variable is a CI variable.
type Variable struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"createdAt,omitzero"` // GitHub only
	UpdatedAt time.Time `json:"updatedAt,omitzero"` // GitHub only
}
