package forgejo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var _ gitoperationslib.History = (*Client)(nil)

// Commits reads commit history. Forgejo reports changed files without
// per-file line counts, and does not detect renames.
func (c *Client) Commits() gitoperationslib.Commits { return commits{c} }

type commits struct{ c *Client }

type commitAuthorJSON struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

type commitJSON struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message   string           `json:"message"`
		Author    commitAuthorJSON `json:"author"`
		Committer commitAuthorJSON `json:"committer"`
	} `json:"commit"`
	Author    *struct{ Login string } `json:"author"`
	Committer *struct{ Login string } `json:"committer"`
	Parents   []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Stats *struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"stats"`
	Files []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	} `json:"files"`
}

func toCommitAuthor(a commitAuthorJSON, account *struct{ Login string }) gitoperationslib.CommitAuthor {
	out := gitoperationslib.CommitAuthor{Name: a.Name, Email: a.Email, Date: a.Date}
	if account != nil {
		out.Login = account.Login
	}
	return out
}

// toCommit converts a commit; withDetails keeps its stats and files.
func (j *commitJSON) toCommit(withDetails bool) gitoperationslib.Commit {
	c := gitoperationslib.Commit{
		SHA: j.SHA, Message: j.Commit.Message, HTMLURL: j.HTMLURL,
		Author:    toCommitAuthor(j.Commit.Author, j.Author),
		Committer: toCommitAuthor(j.Commit.Committer, j.Committer),
		Parents:   make([]string, len(j.Parents)),
	}
	for i, p := range j.Parents {
		c.Parents[i] = p.SHA
	}
	if !withDetails {
		return c
	}
	if j.Stats != nil {
		c.Stats = &gitoperationslib.CommitStats{Additions: j.Stats.Additions, Deletions: j.Stats.Deletions}
	}
	c.Files = make([]gitoperationslib.ChangedFile, len(j.Files))
	for i, f := range j.Files {
		c.Files[i] = gitoperationslib.ChangedFile{Path: f.Filename, Status: gitoperationslib.FileStatus(f.Status)}
	}
	return c
}

func (m commits) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListCommitsOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	if !opts.Since.IsZero() || !opts.Until.IsZero() {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "commits.list", "Forgejo cannot filter commits by date")
	}
	q := forgejoapi.ListQuery(opts.ListOptions)
	// Files, stats and signature checks are expensive and not returned by List.
	q.Set("stat", "false")
	q.Set("files", "false")
	q.Set("verification", "false")
	if opts.Ref != "" {
		q.Set("sha", opts.Ref)
	}
	if opts.Path != "" {
		q.Set("path", opts.Path)
	}
	var out []commitJSON
	resp, err := m.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, repo, "commits"), q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, err
	}
	return toPage(resp, out, func(j *commitJSON) gitoperationslib.Commit { return j.toCommit(false) }), nil
}

func (m commits) Get(ctx context.Context, owner, repo, ref string) (*gitoperationslib.Commit, error) {
	if ref == "" {
		return nil, errors.New("forgejo: commits.get: ref is required")
	}
	q := url.Values{"stat": {"true"}, "files": {"true"}, "verification": {"false"}}
	var out commitJSON
	if _, err := m.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, repo, "git", "commits", ref), q, nil, &out); err != nil {
		return nil, err
	}
	c := out.toCommit(true)
	return &c, nil
}
