package github

import (
	"context"
	"errors"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.History = (*Client)(nil)

// Commits reads commit history. Get reads every page of a commit's files
// (GitHub returns 300 per page, and at most 3000 in all).
func (c *Client) Commits() gitoperationslib.Commits { return commits{c} }

type commits struct{ c *Client }

func toCommitAuthor(a *gh.CommitAuthor, account *gh.User) gitoperationslib.CommitAuthor {
	return gitoperationslib.CommitAuthor{Name: a.GetName(), Email: a.GetEmail(), Login: account.GetLogin(), Date: a.GetDate().Time}
}

func toCommit(rc *gh.RepositoryCommit) gitoperationslib.Commit {
	c := gitoperationslib.Commit{
		SHA: rc.GetSHA(), Message: rc.GetCommit().GetMessage(), HTMLURL: rc.GetHTMLURL(),
		Author:    toCommitAuthor(rc.GetCommit().GetAuthor(), rc.GetAuthor()),
		Committer: toCommitAuthor(rc.GetCommit().GetCommitter(), rc.GetCommitter()),
		Parents:   make([]string, len(rc.Parents)),
	}
	for i, p := range rc.Parents {
		c.Parents[i] = p.GetSHA()
	}
	return c
}

func toChangedFile(f *gh.CommitFile) gitoperationslib.ChangedFile {
	return gitoperationslib.ChangedFile{
		Path: f.GetFilename(), PreviousPath: f.GetPreviousFilename(), Status: gitoperationslib.FileStatus(f.GetStatus()),
		Additions: f.GetAdditions(), Deletions: f.GetDeletions(),
	}
}

func (m commits) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListCommitsOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	if opts.Exclude != "" {
		return m.listExcluding(ctx, owner, repo, opts)
	}
	list, resp, err := m.c.gh.Repositories.ListCommits(ctx, owner, repo, &gh.CommitsListOptions{
		SHA: opts.Ref, Path: opts.Path, Author: opts.Author, Since: opts.Since, Until: opts.Until, ListOptions: ghList(opts.ListOptions),
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, convertError(err)
	}
	return toPage(list, resp, toCommit), nil
}

// listExcluding is git log Exclude..Ref: GitHub's commit list has no such
// filter, so it goes through a comparison, which lists the commits oldest
// first. One request asks for the total, to read them from the end.
func (m commits) listExcluding(ctx context.Context, owner, repo string, opts gitoperationslib.ListCommitsOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	if opts.Path != "" || opts.Author != "" || !opts.Since.IsZero() || !opts.Until.IsZero() {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "commits.list",
			"Exclude cannot be combined with Path, Author, Since or Until")
	}
	ref := opts.Ref
	if ref == "" {
		r, _, err := m.c.gh.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Commit]{}, convertError(err)
		}
		ref = r.GetDefaultBranch()
	}
	cmp, _, err := m.c.gh.Repositories.CompareCommits(ctx, owner, repo, opts.Exclude, ref, &gh.ListOptions{PerPage: 1})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, convertError(err)
	}
	return newestFirst(cmp.GetTotalCommits(), opts.ListOptions, func(lo gh.ListOptions) ([]*gh.RepositoryCommit, error) {
		cmp, _, err := m.c.gh.Repositories.CompareCommits(ctx, owner, repo, opts.Exclude, ref, &lo)
		if err != nil {
			return nil, convertError(err)
		}
		return cmp.Commits, nil
	})
}

func (m commits) Get(ctx context.Context, owner, repo, ref string) (*gitoperationslib.Commit, error) {
	if ref == "" {
		return nil, errors.New("github: commits.get: ref is required")
	}
	opts := &gh.ListOptions{PerPage: 300}
	var c gitoperationslib.Commit
	for {
		rc, resp, err := m.c.gh.Repositories.GetCommit(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, commitError(err)
		}
		if opts.Page == 0 {
			c = toCommit(rc)
			c.Files = []gitoperationslib.ChangedFile{}
			if s := rc.GetStats(); s != nil {
				c.Stats = &gitoperationslib.CommitStats{Additions: s.GetAdditions(), Deletions: s.GetDeletions()}
			}
		}
		for _, f := range rc.Files {
			c.Files = append(c.Files, toChangedFile(f))
		}
		if resp.NextPage == 0 {
			return &c, nil
		}
		opts.Page = resp.NextPage
	}
}
