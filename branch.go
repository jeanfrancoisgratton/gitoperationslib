package gitoperationslib

import "time"

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

// Tag is a repository tag.
type Tag struct {
	Name string `json:"name"`
	SHA  string `json:"sha"` // commit the tag points to (annotated tags are peeled)
	// Message is the annotation of an annotated tag; empty for lightweight
	// tags. GitHub reports it from Get only, not from List.
	Message    string `json:"message,omitzero"`
	TarballURL string `json:"tarballUrl"`
	ZipballURL string `json:"zipballUrl"`
}

// CreateTagOptions describes a tag to create. To publish a release for a
// tag, use Releases().Create, which also creates the tag when needed.
type CreateTagOptions struct {
	Name string `json:"name"` // required
	// Target is the branch, tag or commit SHA to tag; empty means the
	// default branch.
	Target string `json:"target,omitzero"`
	// Message makes an annotated tag; empty makes a lightweight tag.
	Message string `json:"message,omitzero"`
}
