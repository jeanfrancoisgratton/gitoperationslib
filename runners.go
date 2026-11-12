package gitoperationslib

import (
	"context"
	"time"
)

// Runners manages the records of CI runners on the forge: Forgejo Actions and
// GitHub Actions self-hosted runners (CapRunners). Find it with a type assertion:
//
// Only the forge's side: installing and starting a runner's program is left to
// other tools, which take the token or configuration Register returns, or
// the registration token.
type Runners interface {
	List(ctx context.Context, scope RunnerScope, opts ListRunnersOptions) (Page[Runner], error)
	Get(ctx context.Context, scope RunnerScope, id int64) (*Runner, error)
	// Register creates a runner's record, and returns what its program needs
	// to connect. GitHub only registers single-use runners (Ephemeral), and returns
	// ErrNotSupported otherwise: use RegistrationToken and the runner's
	// configuration script for a lasting one.
	Register(ctx context.Context, scope RunnerScope, opts RegisterRunnerOptions) (*RegisteredRunner, error)
	Delete(ctx context.Context, scope RunnerScope, id int64) error
	// RegistrationToken returns a token a runner's program registers with.
	RegistrationToken(ctx context.Context, scope RunnerScope) (*RunnerToken, error)
}

// RunnerScope says whose runners: a repository's, an organisation's (Repo empty),
// the authenticated user's (all empty; Forgejo only), or the whole instance's
// (Instance; Forgejo site admins only).
type RunnerScope struct {
	Owner    string `json:"owner,omitzero"`
	Repo     string `json:"repo,omitzero"`
	Instance bool   `json:"instance,omitzero"`
}

// RepoRunners is the scope of a repository's runners.
func RepoRunners(owner, repo string) RunnerScope { return RunnerScope{Owner: owner, Repo: repo} }

// OrgRunners is the scope of an organisation's runners.
func OrgRunners(org string) RunnerScope { return RunnerScope{Owner: org} }

// UserRunners is the scope of the authenticated user's runners (Forgejo only).
func UserRunners() RunnerScope { return RunnerScope{} }

// InstanceRunners is the scope of the runners every repository of a Forgejo
// instance can use (site admins only).
func InstanceRunners() RunnerScope { return RunnerScope{Instance: true} }

// ListRunnersOptions selects runners.
type ListRunnersOptions struct {
	ListOptions
	// Inherited adds the runners the scope can use from above it, such as a
	// repository's organisation's (Forgejo only).
	Inherited bool `json:"inherited,omitzero"`
}

// Runner is a CI runner as the forge records it.
type Runner struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid,omitzero"` // Forgejo only
	Name        string `json:"name"`
	Description string `json:"description,omitzero"` // Forgejo only
	// Status is the forge's own word: on Forgejo "offline", "idle" or "active",
	// on GitHub "online" or "offline".
	Status    string   `json:"status"`
	Online    bool     `json:"online"`
	Busy      bool     `json:"busy"` // running a job
	Labels    []string `json:"labels,omitzero"`
	Version   string   `json:"version,omitzero"` // of the runner's program, once it connected
	Ephemeral bool     `json:"ephemeral,omitzero"`
	OS        string   `json:"os,omitzero"` // GitHub only
}

// RegisterRunnerOptions describes a runner to register. Names need not be unique.
type RegisterRunnerOptions struct {
	Name        string `json:"name"`                 // required
	Description string `json:"description,omitzero"` // Forgejo only
	// Ephemeral runners take one job, then go away. Required on GitHub.
	Ephemeral bool `json:"ephemeral,omitzero"`
	// Labels the runner's jobs ask for (GitHub only: a Forgejo runner declares its
	// own). Zero value on GitHub: "self-hosted".
	Labels []string `json:"labels,omitzero"`
}

// RegisteredRunner is what a runner's program needs to connect. It is only ever
// returned here, and never serialised.
type RegisteredRunner struct {
	ID   int64  `json:"id"`
	UUID string `json:"uuid,omitzero"` // Forgejo only
	// Token is the runner's secret, for its ".runner" file with ID and UUID
	// (Forgejo only).
	Token string `json:"-"`
	// JITConfig is the encoded configuration for the runner's run.sh --jitconfig
	// (GitHub only).
	JITConfig string `json:"-"`
}

// RunnerToken is a token a runner's program registers with. It is never
// serialised.
type RunnerToken struct {
	Token     string    `json:"-"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"` // GitHub only; Forgejo's does not expire
}
