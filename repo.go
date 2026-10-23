package gitoperationslib

import "time"

// Repo is a repository, modelled on the fields the forges share.
type Repo struct {
	ID            int64      `json:"id"`
	Owner         string     `json:"owner"`
	Name          string     `json:"name"`
	FullName      string     `json:"fullName"` // "owner/name"
	Description   string     `json:"description"`
	Website       string     `json:"website"` // Forgejo "website", GitHub "homepage"
	Visibility    Visibility `json:"visibility"`
	DefaultBranch string     `json:"defaultBranch"`
	Fork          bool       `json:"fork"`
	Mirror        bool       `json:"mirror"`
	Archived      bool       `json:"archived"`
	Template      bool       `json:"template"`
	Empty         bool       `json:"empty,omitzero"` // not reported by GitHub (always false there)
	HTMLURL       string     `json:"htmlUrl"`
	CloneURL      string     `json:"cloneUrl"`
	SSHURL        string     `json:"sshUrl"`
	CreatedAt     time.Time  `json:"createdAt,omitzero"`
	UpdatedAt     time.Time  `json:"updatedAt,omitzero"`
}

// CreateRepoOptions describes a repository to create.
type CreateRepoOptions struct {
	// Owner is the user or organisation that will own the repository; the
	// backend determines which. Empty means the authenticated user. Naming a
	// user other than the authenticated one requires an admin token (Forgejo)
	// and is not supported on GitHub.
	Owner         string     `json:"owner,omitzero"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitzero"`
	Visibility    Visibility `json:"visibility,omitzero"` // zero value: backend default
	DefaultBranch string     `json:"defaultBranch,omitzero"`
	Template      bool       `json:"template,omitzero"`
	// AutoInit creates an initial commit, using Gitignores and License if set.
	AutoInit bool `json:"autoInit,omitzero"`
	// Gitignores lists gitignore template names. GitHub accepts at most one
	// and returns ErrNotSupported otherwise.
	Gitignores []string `json:"gitignores,omitzero"`
	License    string   `json:"license,omitzero"` // license template name
}

// EditRepoOptions changes repository settings. Nil fields are left unchanged;
// use Ptr to set a value.
type EditRepoOptions struct {
	Name          *string     `json:"name,omitzero"`
	Description   *string     `json:"description,omitzero"`
	Website       *string     `json:"website,omitzero"`
	Visibility    *Visibility `json:"visibility,omitzero"`
	DefaultBranch *string     `json:"defaultBranch,omitzero"`
	Archived      *bool       `json:"archived,omitzero"`
	Template      *bool       `json:"template,omitzero"`
}

// Ptr returns a pointer to v, for filling optional fields.
func Ptr[T any](v T) *T { return &v }
