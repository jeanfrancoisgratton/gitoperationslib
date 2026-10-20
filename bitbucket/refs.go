package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

type refJSON struct {
	Name    string `json:"name"`
	Message string `json:"message"` // tags only
	Target  struct {
		Hash string `json:"hash"`
	} `json:"target"`
	Links struct {
		HTML linkJSON `json:"html"`
	} `json:"links"`
}

func (c *Client) defaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, err := c.Repos().Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// resolveCommit turns a branch, tag or commit into a full commit hash; empty
// means the default branch.
func (c *Client) resolveCommit(ctx context.Context, owner, repo, ref string) (string, error) {
	if ref == "" {
		var err error
		if ref, err = c.defaultBranch(ctx, owner, repo); err != nil {
			return "", err
		}
	}
	for _, kind := range []string{"branches", "tags"} {
		var out refJSON
		_, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", kind, ref), nil, nil, &out)
		if err == nil {
			return out.Target.Hash, nil
		}
		if !gitoperationslib.IsNotFound(err) {
			return "", err
		}
	}
	var commit struct {
		Hash string `json:"hash"`
	}
	_, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "commit", ref), nil, nil, &commit)
	if gitoperationslib.IsNotFound(err) {
		return "", &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("%q is not a branch, tag or commit of %s/%s", ref, owner, repo),
		}
	}
	return commit.Hash, err
}

// Branches implements gitoperationslib.Client.
func (c *Client) Branches() gitoperationslib.Branches { return branches{c} }

type branches struct{ c *Client }

func toBranch(r *refJSON, def string) gitoperationslib.Branch {
	return gitoperationslib.Branch{Name: r.Name, SHA: r.Target.Hash, IsDefault: r.Name == def}
}

// List returns the branches. Branch.Protected is not reported by Bitbucket.
func (b branches) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Branch], error) {
	def, err := b.c.defaultBranch(ctx, owner, repo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	page, err := bitbucketapi.List[refJSON](ctx, b.c.api, repoPath(owner, repo, "refs", "branches"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Branch]{Items: make([]gitoperationslib.Branch, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = toBranch(&page.Values[i], def)
	}
	return out, nil
}

func (b branches) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Branch, error) {
	var out refJSON
	if _, err := b.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", "branches", name), nil, nil, &out); err != nil {
		return nil, err
	}
	def, err := b.c.defaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	br := toBranch(&out, def)
	return &br, nil
}

func (b branches) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateBranchOptions) (*gitoperationslib.Branch, error) {
	if opts.Name == "" {
		return nil, errNoName
	}
	sha, err := b.c.resolveCommit(ctx, owner, repo, opts.From)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": opts.Name, "target": map[string]string{"hash": sha}}
	var out refJSON
	if _, err := b.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "refs", "branches"), nil, body, &out); err != nil {
		return nil, err
	}
	br := toBranch(&out, "")
	return &br, nil
}

func (b branches) Rename(context.Context, string, string, string, string) (*gitoperationslib.Branch, error) {
	return nil, notSupported("branches.rename", "Bitbucket has no branch rename API")
}

func (b branches) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := b.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "refs", "branches", name), nil, nil, nil)
	return err
}

// Tags implements gitoperationslib.Client.
func (c *Client) Tags() gitoperationslib.Tags { return tags{c} }

type tags struct{ c *Client }

// toTag reports Bitbucket's generated annotation ("Added tag … for
// changeset …", used when no message was given) as an empty message, like a
// lightweight tag elsewhere.
func toTag(owner, repo string, r *refJSON) gitoperationslib.Tag {
	msg := r.Message
	if strings.HasPrefix(msg, "Added tag "+r.Name+" for changeset ") {
		msg = ""
	}
	archive := "https://bitbucket.org/" + bitbucketapi.Path(owner, repo, "get", r.Name)
	return gitoperationslib.Tag{Name: r.Name, SHA: r.Target.Hash, Message: msg, TarballURL: archive + ".tar.gz", ZipballURL: archive + ".zip"}
}

func (t tags) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Tag], error) {
	page, err := bitbucketapi.List[refJSON](ctx, t.c.api, repoPath(owner, repo, "refs", "tags"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Tag]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Tag]{Items: make([]gitoperationslib.Tag, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = toTag(owner, repo, &page.Values[i])
	}
	return out, nil
}

func (t tags) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Tag, error) {
	var out refJSON
	if _, err := t.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", "tags", name), nil, nil, &out); err != nil {
		return nil, err
	}
	tag := toTag(owner, repo, &out)
	return &tag, nil
}

// Create creates a tag. Bitbucket always creates annotated tags; without a
// Message it writes its own annotation.
func (t tags) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateTagOptions) (*gitoperationslib.Tag, error) {
	if opts.Name == "" {
		return nil, errNoName
	}
	sha, err := t.c.resolveCommit(ctx, owner, repo, opts.Target)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": opts.Name, "target": map[string]string{"hash": sha}}
	if opts.Message != "" {
		body["message"] = opts.Message
	}
	var out refJSON
	if _, err := t.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "refs", "tags"), nil, body, &out); err != nil {
		return nil, err
	}
	tag := toTag(owner, repo, &out)
	return &tag, nil
}

func (t tags) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := t.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "refs", "tags", name), nil, nil, nil)
	return err
}
