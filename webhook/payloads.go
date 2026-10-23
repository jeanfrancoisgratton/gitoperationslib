package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// PushEvent is a push to a branch or tag.
type PushEvent struct {
	Ref        string   `json:"ref"` // "refs/heads/main" or "refs/tags/v1"
	Before     string   `json:"before"`
	After      string   `json:"after"`
	CompareURL string   `json:"compareUrl"`
	Pusher     string   `json:"pusher"` // login
	Commits    []Commit `json:"commits"`
	HeadCommit *Commit  `json:"headCommit,omitzero"` // nil when the push deletes the ref
}

// Branch returns the branch name, or "" if the push is not to a branch.
func (p *PushEvent) Branch() string {
	b, _ := strings.CutPrefix(p.Ref, "refs/heads/")
	if b == p.Ref {
		return ""
	}
	return b
}

// Tag returns the tag name, or "" if the push is not to a tag.
func (p *PushEvent) Tag() string {
	t, _ := strings.CutPrefix(p.Ref, "refs/tags/")
	if t == p.Ref {
		return ""
	}
	return t
}

// Commit is a commit in a push.
type Commit struct {
	ID        string       `json:"id"`
	Message   string       `json:"message"`
	URL       string       `json:"url"`
	Author    CommitAuthor `json:"author"`
	Timestamp time.Time    `json:"timestamp,omitzero"`
	Added     []string     `json:"added"`
	Removed   []string     `json:"removed"`
	Modified  []string     `json:"modified"`
}

// CommitAuthor identifies a commit's author.
type CommitAuthor struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username,omitzero"` // empty if the e-mail matches no account
}

// PullRequestEvent is a change to a pull request.
type PullRequestEvent struct {
	Action      string                       `json:"action"` // "opened", "closed", "reopened", "edited", ...
	Number      int                          `json:"number"`
	PullRequest gitoperationslib.PullRequest `json:"pullRequest"`
}

// ReleaseEvent is a change to a release.
type ReleaseEvent struct {
	Action  string                   `json:"action"` // "published", "updated", "deleted", ...
	Release gitoperationslib.Release `json:"release"`
}

func (e *Event) decode(want string, v any) error {
	if e.Name != want {
		return fmt.Errorf("webhook: %s delivery is not a %s event", e.Name, want)
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("webhook: decode %s payload: %w", want, err)
	}
	return nil
}

type userJSON struct {
	Login string `json:"login"`
	Name  string `json:"name"` // GitHub's pusher has only name and email
}

func (u *userJSON) login() string {
	switch {
	case u == nil:
		return ""
	case u.Login != "":
		return u.Login
	}
	return u.Name
}

type commitJSON struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	URL       string    `json:"url"`
	Timestamp time.Time `json:"timestamp"`
	Author    struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"author"`
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Modified []string `json:"modified"`
}

func (c *commitJSON) toCommit() Commit {
	return Commit{
		ID: c.ID, Message: c.Message, URL: c.URL, Timestamp: c.Timestamp,
		Author: CommitAuthor{Name: c.Author.Name, Email: c.Author.Email, Username: c.Author.Username},
		Added:  c.Added, Removed: c.Removed, Modified: c.Modified,
	}
}

// Push decodes a "push" delivery. For Bitbucket, which can update several
// refs in one delivery, it decodes the first change.
func (e *Event) Push() (*PushEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		if e.Name != "push" {
			return nil, fmt.Errorf("webhook: %s delivery is not a push event", e.Key)
		}
		return e.bitbucketPush()
	}
	var p struct {
		Ref        string       `json:"ref"`
		Before     string       `json:"before"`
		After      string       `json:"after"`
		CompareURL string       `json:"compare_url"` // Forgejo
		Compare    string       `json:"compare"`     // GitHub
		Pusher     *userJSON    `json:"pusher"`
		Commits    []commitJSON `json:"commits"`
		HeadCommit *commitJSON  `json:"head_commit"`
	}
	if err := e.decode("push", &p); err != nil {
		return nil, err
	}
	push := &PushEvent{
		Ref: p.Ref, Before: p.Before, After: p.After, CompareURL: p.CompareURL,
		Pusher: p.Pusher.login(), Commits: make([]Commit, len(p.Commits)),
	}
	if push.CompareURL == "" {
		push.CompareURL = p.Compare
	}
	for i := range p.Commits {
		push.Commits[i] = p.Commits[i].toCommit()
	}
	if p.HeadCommit != nil && p.HeadCommit.ID != "" {
		head := p.HeadCommit.toCommit()
		push.HeadCommit = &head
	}
	return push, nil
}

type branchJSON struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}

func (b *branchJSON) toBranch() gitoperationslib.PRBranch {
	br := gitoperationslib.PRBranch{Ref: b.Ref, SHA: b.SHA}
	if b.Repo != nil {
		br.Repo = b.Repo.FullName
	}
	return br
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// PullRequest decodes a "pull_request" delivery (on Bitbucket, any
// "pullrequest:…" delivery, reviews included).
func (e *Event) PullRequest() (*PullRequestEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		if !isBitbucketPR(e.Key) {
			return nil, fmt.Errorf("webhook: %s delivery is not a pull request event", e.Key)
		}
		return e.bitbucketPullRequest()
	}
	var p struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Number         int        `json:"number"`
			Title          string     `json:"title"`
			Body           string     `json:"body"`
			State          string     `json:"state"`
			Draft          bool       `json:"draft"`
			User           *userJSON  `json:"user"`
			Head           branchJSON `json:"head"`
			Base           branchJSON `json:"base"`
			Mergeable      *bool      `json:"mergeable"`
			Merged         bool       `json:"merged"`
			MergedBy       *userJSON  `json:"merged_by"`
			MergedAt       *time.Time `json:"merged_at"`
			MergeCommitSHA string     `json:"merge_commit_sha"`
			HTMLURL        string     `json:"html_url"`
			CreatedAt      *time.Time `json:"created_at"`
			UpdatedAt      *time.Time `json:"updated_at"`
			ClosedAt       *time.Time `json:"closed_at"`
		} `json:"pull_request"`
	}
	if err := e.decode("pull_request", &p); err != nil {
		return nil, err
	}
	pr := p.PullRequest
	return &PullRequestEvent{
		Action: p.Action,
		Number: p.Number,
		PullRequest: gitoperationslib.PullRequest{
			Number: pr.Number, Title: pr.Title, Body: pr.Body, State: gitoperationslib.PRState(pr.State),
			Draft: pr.Draft, Author: pr.User.login(), Head: pr.Head.toBranch(), Base: pr.Base.toBranch(),
			Mergeable: pr.Mergeable, Merged: pr.Merged, MergedBy: pr.MergedBy.login(), MergedAt: timeOrZero(pr.MergedAt),
			MergeCommitSHA: pr.MergeCommitSHA, HTMLURL: pr.HTMLURL,
			CreatedAt: timeOrZero(pr.CreatedAt), UpdatedAt: timeOrZero(pr.UpdatedAt), ClosedAt: timeOrZero(pr.ClosedAt),
		},
	}, nil
}

// Release decodes a "release" delivery. Assets are not included. Bitbucket
// has no releases.
func (e *Event) Release() (*ReleaseEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return nil, fmt.Errorf("webhook: Bitbucket has no release events (got %s)", e.Key)
	}
	var p struct {
		Action  string `json:"action"`
		Release struct {
			ID              int64      `json:"id"`
			TagName         string     `json:"tag_name"`
			TargetCommitish string     `json:"target_commitish"`
			Name            string     `json:"name"`
			Body            string     `json:"body"`
			Draft           bool       `json:"draft"`
			Prerelease      bool       `json:"prerelease"`
			Author          *userJSON  `json:"author"`
			HTMLURL         string     `json:"html_url"`
			TarballURL      string     `json:"tarball_url"`
			ZipballURL      string     `json:"zipball_url"`
			CreatedAt       *time.Time `json:"created_at"`
			PublishedAt     *time.Time `json:"published_at"`
		} `json:"release"`
	}
	if err := e.decode("release", &p); err != nil {
		return nil, err
	}
	r := p.Release
	rel := gitoperationslib.Release{
		ID: r.ID, TagName: r.TagName, TargetCommitish: r.TargetCommitish, Name: r.Name, Body: r.Body,
		Draft: r.Draft, Prerelease: r.Prerelease, Author: r.Author.login(), HTMLURL: r.HTMLURL,
		TarballURL: r.TarballURL, ZipballURL: r.ZipballURL,
		CreatedAt: timeOrZero(r.CreatedAt), PublishedAt: timeOrZero(r.PublishedAt),
	}
	if rel.Draft || rel.PublishedAt.Unix() <= 0 {
		rel.PublishedAt = time.Time{}
	}
	return &ReleaseEvent{Action: p.Action, Release: rel}, nil
}
