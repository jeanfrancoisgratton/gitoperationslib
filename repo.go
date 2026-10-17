package gitoperationslib

import "time"

// Repo is a repository, modelled on the fields Forgejo and GitHub share.
type Repo struct {
	ID            int64
	Owner         string
	Name          string
	FullName      string // "owner/name"
	Description   string
	Website       string // Forgejo "website", GitHub "homepage"
	Visibility    Visibility
	DefaultBranch string
	Fork          bool
	Mirror        bool
	Archived      bool
	Template      bool
	Empty         bool // not reported by GitHub (always false there)
	HTMLURL       string
	CloneURL      string
	SSHURL        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateRepoOptions describes a repository to create.
type CreateRepoOptions struct {
	// Owner is the user or organisation that will own the repository; the
	// backend determines which. Empty means the authenticated user. Naming a
	// user other than the authenticated one requires an admin token (Forgejo)
	// and is not supported on GitHub.
	Owner         string
	Name          string
	Description   string
	Visibility    Visibility // zero value: backend default
	DefaultBranch string
	Template      bool
	// AutoInit creates an initial commit, using Gitignores and License if set.
	AutoInit bool
	// Gitignores lists gitignore template names. GitHub accepts at most one
	// and returns ErrNotSupported otherwise.
	Gitignores []string
	License    string // license template name
}

// EditRepoOptions changes repository settings. Nil fields are left unchanged;
// use Ptr to set a value.
type EditRepoOptions struct {
	Name          *string
	Description   *string
	Website       *string
	Visibility    *Visibility
	DefaultBranch *string
	Archived      *bool
	Template      *bool
}

// Ptr returns a pointer to v, for filling optional fields.
func Ptr[T any](v T) *T { return &v }
