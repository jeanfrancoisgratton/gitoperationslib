package gitoperationslib

import (
	"context"
	"time"
)

// History reads a repository's commits from the server, like git log: no
// local clone is needed, and only commits that were pushed are seen. The
// Forgejo, GitHub and Bitbucket clients implement it (CapCommits); find it
// with a type assertion:
//
//	h, ok := client.(gitoperationslib.History)
type History interface {
	Commits() Commits
}

// Commits lists and reads commits.
type Commits interface {
	// List returns the commits reachable from opts.Ref, newest first. Their
	// Files and Stats are left empty: use Get for those.
	List(ctx context.Context, owner, repo string, opts ListCommitsOptions) (Page[Commit], error)
	// Get returns one commit with its changed files and line counts. ref is
	// a commit SHA (full or abbreviated), a branch or a tag.
	Get(ctx context.Context, owner, repo, ref string) (*Commit, error)
}

// ListCommitsOptions selects commits.
type ListCommitsOptions struct {
	ListOptions
	// Ref is the branch, tag or commit to start from; empty means the
	// default branch.
	Ref string `json:"ref,omitzero"`
	// Path keeps the commits that touched this file or directory. Forgejo
	// then ignores PerPage.
	Path string `json:"path,omitzero"`
	// Since and Until keep the commits authored in that interval. GitHub
	// only; ErrNotSupported elsewhere.
	Since time.Time `json:"since,omitzero"`
	Until time.Time `json:"until,omitzero"`
}

// Commit is a commit. Files and Stats are only filled by Commits.Get.
type Commit struct {
	SHA     string       `json:"sha"`
	Message string       `json:"message"` // the full message; its first line is the title
	Author  CommitAuthor `json:"author"`
	// Committer is not reported by Bitbucket.
	Committer CommitAuthor `json:"committer,omitzero"`
	Parents   []string     `json:"parents"` // parent SHAs; two or more for a merge
	HTMLURL   string       `json:"htmlUrl"`
	Stats     *CommitStats `json:"stats,omitzero"`
	// Files are the changes against the first parent.
	Files []ChangedFile `json:"files,omitzero"`
}

// CommitAuthor is a commit's author or committer.
type CommitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	// Login is the forge account matched to the e-mail address, if any
	// (Bitbucket: the nickname).
	Login string    `json:"login,omitzero"`
	Date  time.Time `json:"date,omitzero"`
}

// CommitStats counts a commit's changed lines.
type CommitStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// ChangedFile is a file a commit changed.
type ChangedFile struct {
	Path string `json:"path"`
	// PreviousPath is the old path of a renamed file. Forgejo does not
	// detect renames: it reports the old path removed and the new one added.
	PreviousPath string     `json:"previousPath,omitzero"`
	Status       FileStatus `json:"status"`
	// Additions and Deletions count the file's changed lines; always 0 on
	// Forgejo, which only reports the commit's totals (Commit.Stats).
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// FileStatus is how a commit changed a file. Values other than the constants
// (GitHub's "copied", "changed"…) are passed through unchanged.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileRemoved  FileStatus = "removed"
	FileRenamed  FileStatus = "renamed"
)
