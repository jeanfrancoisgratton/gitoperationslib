package gitoperationslib

import (
	"context"
	"io"
	"time"
)

// Commits reads a repository's commits from the server, like git log: no local
// clone is needed, and only commits that were pushed are seen (CapCommits). Diff
// needs CapCommitDiffs, Status and SetStatus CapCommitStatuses: statuses CI
// systems report, and on GitHub check runs.
type Commits interface {
	// List returns the commits reachable from opts.Ref, newest first. Their
	// Files and Stats are left empty: use Get for those.
	List(ctx context.Context, owner, repo string, opts ListCommitsOptions) (Page[Commit], error)
	// Get returns one commit with its changed files and line counts. ref is
	// a commit SHA (full or abbreviated), a branch or a tag.
	Get(ctx context.Context, owner, repo, ref string) (*Commit, error)
	// Diff streams the changes of a commit against its first parent;
	// the caller must close it. ref is a commit SHA, a branch or a tag.
	Diff(ctx context.Context, owner, repo, ref string, format DiffFormat) (io.ReadCloser, error)
	// Status returns the combined state of ref (a branch, tag or commit
	// SHA) and each status it combines.
	Status(ctx context.Context, owner, repo, ref string) (*CombinedStatus, error)
	// SetStatus reports a status on a commit, as a CI system does; a
	// status with the same Context replaces the previous one. Context and State
	// are required; Bitbucket also requires TargetURL. Warnings are Forgejo's
	// only (ErrNotSupported elsewhere), and Check is ignored.
	SetStatus(ctx context.Context, owner, repo, sha string, status CommitStatus) error
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
	// Exclude leaves out the commits reachable from this branch, tag or
	// commit, like git log Exclude..Ref. On GitHub, which needs a comparison
	// for this, it takes one more request per page, and cannot be combined
	// with Path, Since, Until or Author (ErrNotSupported).
	Exclude string `json:"exclude,omitzero"`
	// Author keeps the commits of this account or e-mail address. GitHub
	// only; ErrNotSupported elsewhere.
	Author string `json:"author,omitzero"`
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

// ChangedFile is a file a commit or a pull request changed.
type ChangedFile struct {
	Path string `json:"path"`
	// PreviousPath is the old path of a renamed file. In a commit, Forgejo
	// does not detect renames: it reports the old path removed and the new
	// one added.
	PreviousPath string     `json:"previousPath,omitzero"`
	Status       FileStatus `json:"status"`
	// Additions and Deletions count the file's changed lines. In a commit,
	// they are always 0 on Forgejo, which only reports the totals
	// (Commit.Stats).
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

// DiffFormat selects the format of a raw diff.
type DiffFormat string

const (
	// DiffUnified is a plain unified diff, as git diff prints it.
	DiffUnified DiffFormat = "diff"
	// DiffPatch is the mail-formatted patch git format-patch prints, with
	// each commit's author and message, ready for git am.
	DiffPatch DiffFormat = "patch"
)

// StatusState is the state of a commit status.
type StatusState string

const (
	StatusPending StatusState = "pending" // queued or running
	StatusSuccess StatusState = "success"
	StatusFailure StatusState = "failure" // the checks failed
	StatusError   StatusState = "error"   // the CI system failed, or was stopped
	StatusWarning StatusState = "warning" // Forgejo only
)

// CombinedStatus is the state of a commit, combined from its statuses.
type CombinedStatus struct {
	SHA string `json:"sha"`
	// State is empty when nothing reported on the commit. Otherwise it is a
	// failure or an error if any status is one, pending if any is still
	// running, else a success (Forgejo combines it itself, and has warnings).
	State    StatusState    `json:"state,omitzero"`
	Statuses []CommitStatus `json:"statuses,omitzero"`
}

// CommitStatus is what one CI job, workflow or check reported on a commit.
type CommitStatus struct {
	// Context names it: "ci/woodpecker", "build / test (push)", a check run's
	// name. Bitbucket's key.
	Context     string      `json:"context"`
	State       StatusState `json:"state"`
	Description string      `json:"description,omitzero"`
	TargetURL   string      `json:"targetUrl,omitzero"` // the run's page
	// Check is set for a GitHub check run, which is how GitHub Actions reports.
	Check     bool      `json:"check,omitzero"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// CombineStates combines states as CombinedStatus.State describes; "" for none.
func CombineStates(states ...StatusState) StatusState {
	var out StatusState
	rank := map[StatusState]int{StatusSuccess: 1, StatusWarning: 2, StatusPending: 3, StatusError: 4, StatusFailure: 5}
	for _, s := range states {
		if rank[s] > rank[out] {
			out = s
		}
	}
	return out
}
