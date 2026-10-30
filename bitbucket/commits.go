package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

var _ gitoperationslib.History = (*Client)(nil)

// Commits reads commit history. Bitbucket reports a single date and no
// committer; a commit's files come from its diffstat (one more request).
func (c *Client) Commits() gitoperationslib.Commits { return commits{c} }

type commits struct{ c *Client }

type commitJSON struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Date    time.Time `json:"date"`
	Author  struct {
		Raw  string    `json:"raw"` // "Name <e-mail>"
		User *userJSON `json:"user"`
	} `json:"author"`
	Parents []struct {
		Hash string `json:"hash"`
	} `json:"parents"`
	Links struct {
		HTML linkJSON `json:"html"`
	} `json:"links"`
}

func (j *commitJSON) toCommit() gitoperationslib.Commit {
	c := gitoperationslib.Commit{
		SHA: j.Hash, Message: j.Message, HTMLURL: j.Links.HTML.Href,
		Author:  gitoperationslib.CommitAuthor{Name: j.Author.Raw, Date: j.Date},
		Parents: make([]string, len(j.Parents)),
	}
	if addr, err := mail.ParseAddress(j.Author.Raw); err == nil {
		c.Author.Name, c.Author.Email = addr.Name, addr.Address
	}
	if j.Author.User != nil {
		c.Author.Login = j.Author.User.login()
	}
	for i, p := range j.Parents {
		c.Parents[i] = p.Hash
	}
	return c
}

type diffstatJSON struct {
	Status       string `json:"status"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	Old          *struct {
		Path string `json:"path"`
	} `json:"old"`
	New *struct {
		Path string `json:"path"`
	} `json:"new"`
}

func (d *diffstatJSON) toChangedFile() gitoperationslib.ChangedFile {
	f := gitoperationslib.ChangedFile{Status: gitoperationslib.FileStatus(d.Status), Additions: d.LinesAdded, Deletions: d.LinesRemoved}
	switch {
	case d.New != nil:
		f.Path = d.New.Path
		if d.Old != nil && d.Old.Path != d.New.Path {
			f.PreviousPath = d.Old.Path
		}
	case d.Old != nil: // removed
		f.Path = d.Old.Path
	}
	return f
}

func (m commits) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListCommitsOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	if !opts.Since.IsZero() || !opts.Until.IsZero() {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, notSupported("commits.list", "Bitbucket cannot filter commits by date")
	}
	ref := opts.Ref
	if ref == "" {
		// /commits without a revision lists every branch.
		def, err := m.c.defaultBranch(ctx, owner, repo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Commit]{}, err
		}
		ref = def
	}
	q := bitbucketapi.ListQuery(opts.ListOptions)
	if opts.Path != "" {
		q.Set("path", opts.Path)
	}
	page, err := bitbucketapi.List[commitJSON](ctx, m.c.api, repoPath(owner, repo, "commits", ref), q)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Commit]{Items: make([]gitoperationslib.Commit, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toCommit()
	}
	return out, nil
}

func (m commits) Get(ctx context.Context, owner, repo, ref string) (*gitoperationslib.Commit, error) {
	if ref == "" {
		return nil, errors.New("bitbucket: commits.get: ref is required")
	}
	j, err := m.resolve(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	c := j.toCommit()
	stats, err := bitbucketapi.All[diffstatJSON](ctx, m.c.api, repoPath(owner, repo, "diffstat", j.Hash), url.Values{"pagelen": {"500"}})
	if err != nil {
		return nil, err
	}
	c.Stats = &gitoperationslib.CommitStats{}
	c.Files = make([]gitoperationslib.ChangedFile, len(stats))
	for i := range stats {
		c.Files[i] = stats[i].toChangedFile()
		c.Stats.Additions += stats[i].LinesAdded
		c.Stats.Deletions += stats[i].LinesRemoved
	}
	return &c, nil
}

// resolve reads the commit ref points to. /commit/{ref} does not resolve
// branch names containing "/" (404, checked 2026-10-30); the commit list
// does, so a 404 falls back to its first entry.
func (m commits) resolve(ctx context.Context, owner, repo, ref string) (*commitJSON, error) {
	var j commitJSON
	_, err := m.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "commit", ref), nil, nil, &j)
	if !gitoperationslib.IsNotFound(err) {
		return &j, err
	}
	page, listErr := bitbucketapi.List[commitJSON](ctx, m.c.api, repoPath(owner, repo, "commits", ref), url.Values{"pagelen": {"1"}})
	if listErr != nil || len(page.Values) == 0 {
		return nil, err // the original 404
	}
	return &page.Values[0], nil
}
