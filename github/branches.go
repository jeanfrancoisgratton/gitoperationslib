package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// resolveCommit turns a branch, tag or commit SHA into a commit SHA; empty
// means the default branch. Annotated tags are peeled to their commit.
func (c *Client) resolveCommit(ctx context.Context, owner, repo, ref string) (string, error) {
	if ref == "" {
		r, _, err := c.gh.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return "", convertError(err)
		}
		ref = r.GetDefaultBranch()
	}
	for _, prefix := range []string{"heads/", "tags/"} {
		r, _, err := c.gh.Git.GetRef(ctx, owner, repo, prefix+ref)
		if err != nil {
			if gitoperationslib.IsNotFound(convertError(err)) {
				continue
			}
			return "", convertError(err)
		}
		return c.peel(ctx, owner, repo, r.GetObject())
	}
	// Not a branch or tag: accept it as a commit if it resolves to one.
	commit, _, err := c.gh.Git.GetCommit(ctx, owner, repo, ref)
	if err != nil {
		if gitoperationslib.IsNotFound(convertError(err)) || gitoperationslib.StatusCode(convertError(err)) == http.StatusUnprocessableEntity {
			return "", &gitoperationslib.APIError{
				Backend: gitoperationslib.BackendGitHub, StatusCode: http.StatusNotFound,
				Message: fmt.Sprintf("%q is not a branch, tag or commit of %s/%s", ref, owner, repo),
			}
		}
		return "", convertError(err)
	}
	return commit.GetSHA(), nil
}

// peel follows annotated tag objects to the commit they point at.
func (c *Client) peel(ctx context.Context, owner, repo string, obj *gh.GitObject) (string, error) {
	for obj.GetType() == "tag" {
		tag, _, err := c.gh.Git.GetTag(ctx, owner, repo, obj.GetSHA())
		if err != nil {
			return "", convertError(err)
		}
		obj = tag.GetObject()
	}
	return obj.GetSHA(), nil
}

func toRepoBranch(b *gh.Branch, def string) gitoperationslib.Branch {
	return gitoperationslib.Branch{Name: b.GetName(), SHA: b.GetCommit().GetSHA(), Protected: b.GetProtected(), IsDefault: b.GetName() == def}
}

// Branches implements gitoperationslib.Client.
func (c *Client) Branches() gitoperationslib.Branches { return branches{c} }

type branches struct{ c *Client }

func (b branches) defaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := b.c.gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", convertError(err)
	}
	return r.GetDefaultBranch(), nil
}

func (b branches) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Branch], error) {
	def, err := b.defaultBranch(ctx, owner, repo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	list, resp, err := b.c.gh.Repositories.ListBranches(ctx, owner, repo, &gh.BranchListOptions{
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Branch]{Items: make([]gitoperationslib.Branch, len(list)), NextPage: resp.NextPage}
	for i, br := range list {
		page.Items[i] = toRepoBranch(br, def)
	}
	return page, nil
}

func (b branches) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Branch, error) {
	// No redirects: after a rename, GitHub would otherwise answer for the
	// old name with the renamed branch.
	br, resp, err := b.c.gh.Repositories.GetBranch(ctx, owner, repo, name, 0)
	if err != nil {
		// GetBranch reports HTTP errors as plain errors; rebuild an APIError
		// from the response. A redirect means the branch was renamed.
		if resp != nil && resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			if status == http.StatusMovedPermanently {
				status = http.StatusNotFound
			}
			return nil, &gitoperationslib.APIError{
				Backend: gitoperationslib.BackendGitHub, StatusCode: status,
				Message: fmt.Sprintf("branch %q not found in %s/%s", name, owner, repo),
			}
		}
		return nil, convertError(err)
	}
	def, err := b.defaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	out := toRepoBranch(br, def)
	return &out, nil
}

func (b branches) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateBranchOptions) (*gitoperationslib.Branch, error) {
	if opts.Name == "" {
		return nil, errors.New("github: branches.create: name is required")
	}
	sha, err := b.c.resolveCommit(ctx, owner, repo, opts.From)
	if err != nil {
		return nil, err
	}
	if _, _, err := b.c.gh.Git.CreateRef(ctx, owner, repo, gh.CreateRef{Ref: "refs/heads/" + opts.Name, SHA: sha}); err != nil {
		return nil, convertError(err)
	}
	return &gitoperationslib.Branch{Name: opts.Name, SHA: sha}, nil
}

func (b branches) Rename(ctx context.Context, owner, repo, name, newName string) (*gitoperationslib.Branch, error) {
	if newName == "" {
		return nil, errors.New("github: branches.rename: new name is required")
	}
	if _, _, err := b.c.gh.Repositories.RenameBranch(ctx, owner, repo, name, newName); err != nil {
		return nil, convertError(err)
	}
	return b.Get(ctx, owner, repo, newName)
}

func (b branches) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := b.c.gh.Git.DeleteRef(ctx, owner, repo, "heads/"+name)
	return convertError(err)
}

// Tags implements gitoperationslib.Client.
func (c *Client) Tags() gitoperationslib.Tags { return tags{c} }

type tags struct{ c *Client }

func (t tags) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Tag], error) {
	list, resp, err := t.c.gh.Repositories.ListTags(ctx, owner, repo, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Tag]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Tag]{Items: make([]gitoperationslib.Tag, len(list)), NextPage: resp.NextPage}
	for i, tg := range list {
		page.Items[i] = gitoperationslib.Tag{
			Name: tg.GetName(), SHA: tg.GetCommit().GetSHA(), TarballURL: tg.GetTarballURL(), ZipballURL: tg.GetZipballURL(),
		}
	}
	return page, nil
}

func (t tags) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Tag, error) {
	ref, _, err := t.c.gh.Git.GetRef(ctx, owner, repo, "tags/"+name)
	if err != nil {
		return nil, convertError(err)
	}
	tag := &gitoperationslib.Tag{
		Name:       name,
		TarballURL: fmt.Sprintf("%srepos/%s/%s/tarball/refs/tags/%s", t.c.gh.BaseURL(), owner, repo, name),
		ZipballURL: fmt.Sprintf("%srepos/%s/%s/zipball/refs/tags/%s", t.c.gh.BaseURL(), owner, repo, name),
	}
	obj := ref.GetObject()
	if obj.GetType() == "tag" {
		annotated, _, err := t.c.gh.Git.GetTag(ctx, owner, repo, obj.GetSHA())
		if err != nil {
			return nil, convertError(err)
		}
		tag.Message = annotated.GetMessage()
		obj = annotated.GetObject()
	}
	if tag.SHA, err = t.c.peel(ctx, owner, repo, obj); err != nil {
		return nil, err
	}
	return tag, nil
}

func (t tags) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateTagOptions) (*gitoperationslib.Tag, error) {
	if opts.Name == "" {
		return nil, errors.New("github: tags.create: name is required")
	}
	sha, err := t.c.resolveCommit(ctx, owner, repo, opts.Target)
	if err != nil {
		return nil, err
	}
	refSHA := sha
	if opts.Message != "" {
		// An annotated tag is a tag object plus a ref pointing at it.
		obj, _, err := t.c.gh.Git.CreateTag(ctx, owner, repo, gh.CreateTag{Tag: opts.Name, Message: opts.Message, Object: sha, Type: "commit"})
		if err != nil {
			return nil, convertError(err)
		}
		refSHA = obj.GetSHA()
	}
	if _, _, err := t.c.gh.Git.CreateRef(ctx, owner, repo, gh.CreateRef{Ref: "refs/tags/" + opts.Name, SHA: refSHA}); err != nil {
		return nil, convertError(err)
	}
	return t.Get(ctx, owner, repo, opts.Name)
}

func (t tags) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := t.c.gh.Git.DeleteRef(ctx, owner, repo, "tags/"+name)
	return convertError(err)
}
