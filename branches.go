package gitoperationslib

import (
	"context"
	"time"
)

// Branch is a repository branch.
type Branch struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"` // head commit
	Protected bool   `json:"protected"`
	IsDefault bool   `json:"isDefault"`
	// LastCommit describes the head commit. Get always reports it; List
	// does on Forgejo and Bitbucket, but not on GitHub, whose branch list
	// carries the commit SHA alone (nil there).
	LastCommit *CommitSummary `json:"lastCommit,omitzero"`
}

// CommitSummary describes a commit.
type CommitSummary struct {
	Message string    `json:"message"` // the full message; its first line is the title
	Author  string    `json:"author"`  // the author's name
	Date    time.Time `json:"date"`    // the author date
}

// Comparison tells how far apart two refs are.
type Comparison struct {
	Base   string `json:"base"`
	Head   string `json:"head"`
	Ahead  int    `json:"ahead"`  // commits in Head that Base lacks
	Behind int    `json:"behind"` // commits in Base that Head lacks
}

// CreateBranchOptions describes a branch to create.
type CreateBranchOptions struct {
	Name string `json:"name"` // required
	// From is the branch, tag or commit SHA to start from; empty means the
	// default branch.
	From string `json:"from,omitzero"`
}

// Branches manages repository branches. The default branch is changed with
// Repos().Edit.
type Branches interface {
	// List returns the branches; IsDefault marks the default one.
	List(ctx context.Context, owner, repo string, opts ListOptions) (Page[Branch], error)
	Get(ctx context.Context, owner, repo, name string) (*Branch, error)
	Create(ctx context.Context, owner, repo string, opts CreateBranchOptions) (*Branch, error)
	// Rename renames a branch. Open pull requests and protection rules
	// follow it.
	Rename(ctx context.Context, owner, repo, name, newName string) (*Branch, error)
	Delete(ctx context.Context, owner, repo, name string) error
	// Compare counts the commits between base and head (branches, tags or
	// commits): Ahead, those in head but not in base, and Behind, the
	// reverse. Both are 0 when head and base point to the same commit.
	Compare(ctx context.Context, owner, repo, base, head string) (*Comparison, error)
}
