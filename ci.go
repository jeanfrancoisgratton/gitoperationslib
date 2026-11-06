package gitoperationslib

import (
	"context"
	"time"
)

// CIScope selects where CI secrets and variables live: a repository, one of its
// environments, an organisation or workspace (Repo empty), or the authenticated
// user (all empty).
type CIScope struct {
	Owner       string `json:"owner,omitzero"`
	Repo        string `json:"repo,omitzero"`
	Environment string `json:"environment,omitzero"`
}

// RepoCI is the scope of a repository's secrets and variables.
func RepoCI(owner, repo string) CIScope { return CIScope{Owner: owner, Repo: repo} }

// EnvCI is the scope of the secrets and variables of a repository's environment,
// on GitHub and Bitbucket (CapCIEnvironments); Forgejo has no environments and
// returns ErrNotSupported. The environment must exist: anything else is HTTP 404.
// The library does not create environments. A Bitbucket environment is named by
// its name or slug, ignoring case, or by its {UUID}; each call looks it up first.
// On a GitHub repository that is private, environments may need a paid plan.
func EnvCI(owner, repo, environment string) CIScope {
	return CIScope{Owner: owner, Repo: repo, Environment: environment}
}

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
	// List returns the secrets' names. Forgejo before 17 cannot list the user's
	// secrets (ErrNotSupported).
	List(ctx context.Context, scope CIScope, opts ListOptions) (Page[Secret], error)
	// Set creates the secret or replaces its value. On GitHub the value is
	// encrypted for the scope's public key before it is sent. A new
	// organisation secret is available to all the organisation's repositories,
	// and an existing one keeps its access, at the cost of one or two more
	// requests: see OrgSecrets to limit it.
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

// SecretVisibility is which of an organisation's repositories can use a secret.
type SecretVisibility string

const (
	SecretVisibleAll      SecretVisibility = "all"      // all of them
	SecretVisiblePrivate  SecretVisibility = "private"  // the private and internal ones
	SecretVisibleSelected SecretVisibility = "selected" // those listed in SecretAccess.Repos
)

// SecretAccess is which of an organisation's repositories can use a secret.
type SecretAccess struct {
	Visibility SecretVisibility `json:"visibility"`
	// Repos names the repositories, without their owner. It only applies to
	// SecretVisibleSelected; empty means none.
	Repos []string `json:"repos,omitzero"`
}

// OrgSecrets limits organisation secrets to some repositories. Only the GitHub
// backend implements it (CapOrgSecrets): the secrets of a Forgejo organisation or
// a Bitbucket workspace are available to all its repositories. Find it with a
// type assertion:
//
//	s, ok := client.(gitoperationslib.OrgSecrets)
//
// Each repository named takes one more request, to find its ID. A secret's value
// cannot be read back, so changing its visibility takes SetOrgSecret, with the
// value.
type OrgSecrets interface {
	// GetOrgSecretAccess returns who can use a secret.
	GetOrgSecretAccess(ctx context.Context, org, name string) (*SecretAccess, error)
	// SetOrgSecret creates a secret or replaces it, with its value and access.
	SetOrgSecret(ctx context.Context, org, name, value string, access SecretAccess) error
	// SetOrgSecretRepos replaces the repositories that can use a secret. GitHub
	// answers HTTP 409 unless its visibility is SecretVisibleSelected.
	SetOrgSecretRepos(ctx context.Context, org, name string, repos []string) error
}
