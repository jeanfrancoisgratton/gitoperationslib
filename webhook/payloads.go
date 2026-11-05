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

// IssuesEvent is a change to an issue. Pull requests have their own events.
type IssuesEvent struct {
	Action string                 `json:"action"` // "opened", "edited", "closed", "reopened", ...
	Issue  gitoperationslib.Issue `json:"issue"`
}

// IssueCommentEvent is a change to a comment in the conversation of an issue or
// a pull request. Comments on a pull request's lines are ReviewCommentEvents.
type IssueCommentEvent struct {
	Action string `json:"action"` // "created", "edited" or "deleted"
	// Issue is the issue or, when IsPullRequest is set, the pull request, under
	// the same number.
	Issue         gitoperationslib.Issue   `json:"issue"`
	IsPullRequest bool                     `json:"isPullRequest,omitzero"`
	Comment       gitoperationslib.Comment `json:"comment"`
}

// ReviewEvent is a review of a pull request.
type ReviewEvent struct {
	// Action is "submitted" or "dismissed", or on GitHub also "edited". All
	// forges report the verdict in Review.State.
	Action      string                       `json:"action"`
	Number      int                          `json:"number"`
	PullRequest gitoperationslib.PullRequest `json:"pullRequest"`
	// Review holds what the forge sends. GitHub: all of it. Forgejo: Reviewer,
	// State and Body. Bitbucket, whose approvals have no text: Reviewer, State
	// and SubmittedAt.
	Review gitoperationslib.Review `json:"review"`
}

// ReviewCommentEvent is a change to a comment on a line of a pull request.
// Forgejo has no such event: a review's delivery does not list its comments.
type ReviewCommentEvent struct {
	Action      string                         `json:"action"` // "created", "edited" or "deleted"
	Number      int                            `json:"number"`
	PullRequest gitoperationslib.PullRequest   `json:"pullRequest"`
	Comment     gitoperationslib.ReviewComment `json:"comment"`
}

// RefEvent is a branch or tag being created or deleted.
type RefEvent struct {
	RefType string `json:"refType"`      // "branch" or "tag"
	Ref     string `json:"ref"`          // the short name: "main", "v1"
	SHA     string `json:"sha,omitzero"` // the commit; only Forgejo sends it, on create
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
	Label string `json:"label"` // Forgejo: the branch name; GitHub: "owner:branch"
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	Repo  *struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}

func (b *branchJSON) toBranch() gitoperationslib.PRBranch {
	// Once the head branch is deleted, Forgejo sends the pull request's own ref (refs/pull/N/head) as ref; its
	// label still holds the branch name. GitHub always sends the branch name as ref.
	ref := b.Ref
	if strings.HasPrefix(ref, "refs/pull/") && b.Label != "" {
		ref = b.Label
	}
	br := gitoperationslib.PRBranch{Ref: ref, SHA: b.SHA}
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

type prJSON struct {
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
}

func (pr *prJSON) toPullRequest() gitoperationslib.PullRequest {
	return gitoperationslib.PullRequest{
		Number: pr.Number, Title: pr.Title, Body: pr.Body, State: gitoperationslib.PRState(pr.State),
		Draft: pr.Draft, Author: pr.User.login(), Head: pr.Head.toBranch(), Base: pr.Base.toBranch(),
		Mergeable: pr.Mergeable, Merged: pr.Merged, MergedBy: pr.MergedBy.login(), MergedAt: timeOrZero(pr.MergedAt),
		MergeCommitSHA: pr.MergeCommitSHA, HTMLURL: pr.HTMLURL,
		CreatedAt: timeOrZero(pr.CreatedAt), UpdatedAt: timeOrZero(pr.UpdatedAt), ClosedAt: timeOrZero(pr.ClosedAt),
	}
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
		PullRequest prJSON `json:"pull_request"`
	}
	if err := e.decode("pull_request", &p); err != nil {
		return nil, err
	}
	return &PullRequestEvent{Action: p.Action, Number: p.Number, PullRequest: p.PullRequest.toPullRequest()}, nil
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

type milestoneJSON struct {
	ID           int64      `json:"id"`
	Number       int64      `json:"number"` // GitHub
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	DueOn        *time.Time `json:"due_on"`
	OpenIssues   int        `json:"open_issues"`
	ClosedIssues int        `json:"closed_issues"`
	ClosedAt     *time.Time `json:"closed_at"`
}

func (m *milestoneJSON) toMilestone() *gitoperationslib.Milestone {
	if m == nil {
		return nil
	}
	id := m.ID
	if m.Number != 0 { // what addresses a milestone on GitHub
		id = m.Number
	}
	return &gitoperationslib.Milestone{
		ID: id, Title: m.Title, Description: m.Description, State: gitoperationslib.MilestoneState(m.State),
		DueOn: timeOrZero(m.DueOn), OpenIssues: m.OpenIssues, ClosedIssues: m.ClosedIssues, ClosedAt: timeOrZero(m.ClosedAt),
	}
}

type issueJSON struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	State  string    `json:"state"`
	User   *userJSON `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []*userJSON    `json:"assignees"`
	Milestone *milestoneJSON `json:"milestone"`
	Comments  int            `json:"comments"`
	Locked    bool           `json:"locked"`    // GitHub
	IsLocked  bool           `json:"is_locked"` // Forgejo
	HTMLURL   string         `json:"html_url"`
	CreatedAt *time.Time     `json:"created_at"`
	UpdatedAt *time.Time     `json:"updated_at"`
	ClosedAt  *time.Time     `json:"closed_at"`
	// PullRequest is set when the "issue" is a pull request.
	PullRequest *json.RawMessage `json:"pull_request"`
}

func (i *issueJSON) toIssue() gitoperationslib.Issue {
	issue := gitoperationslib.Issue{
		Number: i.Number, Title: i.Title, Body: i.Body, State: gitoperationslib.IssueState(i.State), Author: i.User.login(),
		Labels: make([]string, len(i.Labels)), Assignees: make([]string, len(i.Assignees)), Milestone: i.Milestone.toMilestone(),
		Comments: i.Comments, Locked: i.Locked || i.IsLocked, HTMLURL: i.HTMLURL,
		CreatedAt: timeOrZero(i.CreatedAt), UpdatedAt: timeOrZero(i.UpdatedAt), ClosedAt: timeOrZero(i.ClosedAt),
	}
	for n := range i.Labels {
		issue.Labels[n] = i.Labels[n].Name
	}
	for n, a := range i.Assignees {
		issue.Assignees[n] = a.login()
	}
	return issue
}

// Issues decodes an "issues" delivery. Bitbucket has no issues.
func (e *Event) Issues() (*IssuesEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return nil, fmt.Errorf("webhook: Bitbucket has no issue events (got %s)", e.Key)
	}
	var p struct {
		Action string    `json:"action"`
		Issue  issueJSON `json:"issue"`
	}
	if err := e.decode("issues", &p); err != nil {
		return nil, err
	}
	return &IssuesEvent{Action: p.Action, Issue: p.Issue.toIssue()}, nil
}

// IssueComment decodes an "issue_comment" delivery. On Bitbucket, those are the
// comments on a pull request that are not on one of its lines.
func (e *Event) IssueComment() (*IssueCommentEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return e.bitbucketIssueComment()
	}
	var p struct {
		Action  string    `json:"action"`
		Issue   issueJSON `json:"issue"`
		Comment struct {
			ID        int64      `json:"id"`
			User      *userJSON  `json:"user"`
			Body      string     `json:"body"`
			HTMLURL   string     `json:"html_url"`
			CreatedAt *time.Time `json:"created_at"`
			UpdatedAt *time.Time `json:"updated_at"`
		} `json:"comment"`
	}
	if err := e.decode("issue_comment", &p); err != nil {
		return nil, err
	}
	c := p.Comment
	return &IssueCommentEvent{
		Action: p.Action, Issue: p.Issue.toIssue(), IsPullRequest: p.Issue.PullRequest != nil,
		Comment: gitoperationslib.Comment{
			ID: c.ID, Author: c.User.login(), Body: c.Body, HTMLURL: c.HTMLURL,
			CreatedAt: timeOrZero(c.CreatedAt), UpdatedAt: timeOrZero(c.UpdatedAt),
		},
	}, nil
}

// forgejoReviews gives the verdict of the events Forgejo sends for reviews.
var forgejoReviews = map[string]gitoperationslib.ReviewState{
	"pull_request_approved": gitoperationslib.ReviewApproved,
	"pull_request_rejected": gitoperationslib.ReviewChangesRequested,
	"pull_request_comment":  gitoperationslib.ReviewCommented,
}

// Review decodes a "pull_request_review" delivery.
func (e *Event) Review() (*ReviewEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return e.bitbucketReview()
	}
	var p struct {
		Action      string `json:"action"`
		Number      int    `json:"number"` // Forgejo
		PullRequest prJSON `json:"pull_request"`
		Review      struct {
			ID          int64      `json:"id"`
			User        *userJSON  `json:"user"`
			Body        string     `json:"body"`
			Content     string     `json:"content"` // Forgejo
			State       string     `json:"state"`
			CommitID    string     `json:"commit_id"`
			HTMLURL     string     `json:"html_url"`
			SubmittedAt *time.Time `json:"submitted_at"`
		} `json:"review"`
	}
	if err := e.decode("pull_request_review", &p); err != nil {
		return nil, err
	}
	r := p.Review
	out := &ReviewEvent{
		Action: p.Action, Number: p.PullRequest.Number, PullRequest: p.PullRequest.toPullRequest(),
		Review: gitoperationslib.Review{
			ID: r.ID, Reviewer: r.User.login(), State: gitoperationslib.ReviewState(r.State), Body: r.Body,
			CommitID: r.CommitID, HTMLURL: r.HTMLURL, SubmittedAt: timeOrZero(r.SubmittedAt),
		},
	}
	if e.Forge == gitoperationslib.BackendForgejo {
		out.Action = e.Action
		out.Review.Reviewer, out.Review.State, out.Review.Body = e.Sender, forgejoReviews[e.Key], r.Content
	}
	return out, nil
}

// ReviewComment decodes a "pull_request_review_comment" delivery, which Forgejo
// does not send.
func (e *Event) ReviewComment() (*ReviewCommentEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return e.bitbucketReviewComment()
	}
	var p struct {
		Action      string `json:"action"`
		PullRequest prJSON `json:"pull_request"`
		Comment     struct {
			ID           int64      `json:"id"`
			User         *userJSON  `json:"user"`
			Body         string     `json:"body"`
			Path         string     `json:"path"`
			Line         int        `json:"line"`
			OriginalLine int        `json:"original_line"`
			Side         string     `json:"side"`
			CommitID     string     `json:"commit_id"`
			ReviewID     int64      `json:"pull_request_review_id"`
			HTMLURL      string     `json:"html_url"`
			CreatedAt    *time.Time `json:"created_at"`
			UpdatedAt    *time.Time `json:"updated_at"`
		} `json:"comment"`
	}
	if err := e.decode("pull_request_review_comment", &p); err != nil {
		return nil, err
	}
	c := p.Comment
	comment := gitoperationslib.ReviewComment{
		ID: c.ID, Author: c.User.login(), Body: c.Body, Path: c.Path, CommitID: c.CommitID, ReviewID: c.ReviewID,
		HTMLURL: c.HTMLURL, CreatedAt: timeOrZero(c.CreatedAt), UpdatedAt: timeOrZero(c.UpdatedAt),
	}
	// An outdated comment has no current line, only the original one.
	line := c.Line
	if line == 0 {
		line = c.OriginalLine
	}
	if c.Side == "LEFT" {
		comment.OldLine = line
	} else {
		comment.Line = line
	}
	return &ReviewCommentEvent{Action: p.Action, Number: p.PullRequest.Number, PullRequest: p.PullRequest.toPullRequest(), Comment: comment}, nil
}

func (e *Event) ref(want string) (*RefEvent, error) {
	if e.Forge == gitoperationslib.BackendBitbucket {
		return nil, fmt.Errorf("webhook: Bitbucket has no %s events: it reports them as a push, with no old or new commit (got %s)", want, e.Key)
	}
	var p struct {
		Ref     string `json:"ref"`
		RefType string `json:"ref_type"`
		SHA     string `json:"sha"`
	}
	if err := e.decode(want, &p); err != nil {
		return nil, err
	}
	// Forgejo sends the full ref, GitHub the short name.
	ref := strings.TrimPrefix(strings.TrimPrefix(p.Ref, "refs/heads/"), "refs/tags/")
	return &RefEvent{RefType: p.RefType, Ref: ref, SHA: p.SHA}, nil
}

// Create decodes a "create" delivery: a branch or tag was created.
func (e *Event) Create() (*RefEvent, error) { return e.ref("create") }

// Delete decodes a "delete" delivery: a branch or tag was deleted.
func (e *Event) Delete() (*RefEvent, error) { return e.ref("delete") }
