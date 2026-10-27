package forgejo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type branchJSON struct {
	Name   string `json:"name"`
	Commit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
		Timestamp time.Time `json:"timestamp"` // the author date
	} `json:"commit"`
	Protected bool `json:"protected"`
}

func (b *branchJSON) toBranch() gitoperationslib.Branch {
	br := gitoperationslib.Branch{Name: b.Name, SHA: b.Commit.ID, Protected: b.Protected}
	if !b.Commit.Timestamp.IsZero() {
		br.LastCommit = &gitoperationslib.CommitSummary{Message: b.Commit.Message, Author: b.Commit.Author.Name, Date: b.Commit.Timestamp}
	}
	return br
}

func branchesPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "branches"}, rest...)...)
}

// Branches implements gitoperationslib.Client.
func (c *Client) Branches() gitoperationslib.Branches { return branches{c} }

type branches struct{ c *Client }

func (b branches) defaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, err := b.c.Repos().Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

func (b branches) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Branch], error) {
	def, err := b.defaultBranch(ctx, owner, repo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	var out []branchJSON
	resp, err := b.c.api.Do(ctx, http.MethodGet, branchesPath(owner, repo), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	page := toPage(resp, out, (*branchJSON).toBranch)
	for i := range page.Items {
		page.Items[i].IsDefault = page.Items[i].Name == def
	}
	return page, nil
}

func (b branches) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Branch, error) {
	var out branchJSON
	if _, err := b.c.api.Do(ctx, http.MethodGet, branchesPath(owner, repo, name), nil, nil, &out); err != nil {
		return nil, err
	}
	def, err := b.defaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	br := out.toBranch()
	br.IsDefault = br.Name == def
	return &br, nil
}

func (b branches) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateBranchOptions) (*gitoperationslib.Branch, error) {
	if opts.Name == "" {
		return nil, errors.New("forgejo: branches.create: name is required")
	}
	// old_ref_name accepts a branch, tag or commit; empty means the default
	// branch.
	body := struct {
		Name string `json:"new_branch_name"`
		From string `json:"old_ref_name,omitempty"`
	}{opts.Name, opts.From}
	var out branchJSON
	if _, err := b.c.api.Do(ctx, http.MethodPost, branchesPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	br := out.toBranch()
	return &br, nil
}

func (b branches) Rename(ctx context.Context, owner, repo, name, newName string) (*gitoperationslib.Branch, error) {
	if newName == "" {
		return nil, errors.New("forgejo: branches.rename: new name is required")
	}
	body := struct {
		Name string `json:"name"`
	}{newName}
	if _, err := b.c.api.Do(ctx, http.MethodPatch, branchesPath(owner, repo, name), nil, body, nil); err != nil {
		return nil, err
	}
	return b.Get(ctx, owner, repo, newName)
}

func (b branches) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := b.c.api.Do(ctx, http.MethodDelete, branchesPath(owner, repo, name), nil, nil, nil)
	return err
}

// Compare asks twice: Forgejo only counts the commits of head that base lacks.
func (b branches) Compare(ctx context.Context, owner, repo, base, head string) (*gitoperationslib.Comparison, error) {
	if base == "" || head == "" {
		return nil, errors.New("forgejo: branches.compare: base and head are required")
	}
	ahead, err := b.commitsMissing(ctx, owner, repo, base, head)
	if err != nil {
		return nil, err
	}
	behind, err := b.commitsMissing(ctx, owner, repo, head, base)
	if err != nil {
		return nil, err
	}
	return &gitoperationslib.Comparison{Base: base, Head: head, Ahead: ahead, Behind: behind}, nil
}

// commitsMissing counts the commits of head that base lacks.
func (b branches) commitsMissing(ctx context.Context, owner, repo, base, head string) (int, error) {
	var out struct {
		TotalCommits int `json:"total_commits"`
	}
	path := forgejoapi.Path("repos", owner, repo, "compare", base+"..."+head)
	if _, err := b.c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return 0, err
	}
	return out.TotalCommits, nil
}

type tagJSON struct {
	ID      string `json:"id"` // tag object SHA for annotated tags, commit SHA otherwise
	Name    string `json:"name"`
	Message string `json:"message"`
	Commit  struct {
		SHA string `json:"sha"`
	} `json:"commit"`
	TarballURL string `json:"tarball_url"`
	ZipballURL string `json:"zipball_url"`
}

func (t *tagJSON) toTag() gitoperationslib.Tag {
	tag := gitoperationslib.Tag{Name: t.Name, SHA: t.Commit.SHA, TarballURL: t.TarballURL, ZipballURL: t.ZipballURL}
	// For lightweight tags Forgejo reports the commit message; keep the
	// message only for annotated tags, whose ID is the tag object's.
	if t.ID != "" && t.ID != t.Commit.SHA {
		tag.Message = t.Message
	}
	return tag
}

func tagsPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "tags"}, rest...)...)
}

// Tags implements gitoperationslib.Client.
func (c *Client) Tags() gitoperationslib.Tags { return tags{c} }

type tags struct{ c *Client }

func (t tags) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Tag], error) {
	var out []tagJSON
	resp, err := t.c.api.Do(ctx, http.MethodGet, tagsPath(owner, repo), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Tag]{}, err
	}
	return toPage(resp, out, (*tagJSON).toTag), nil
}

func (t tags) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Tag, error) {
	var out tagJSON
	if _, err := t.c.api.Do(ctx, http.MethodGet, tagsPath(owner, repo, name), nil, nil, &out); err != nil {
		return nil, err
	}
	tag := out.toTag()
	return &tag, nil
}

func (t tags) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateTagOptions) (*gitoperationslib.Tag, error) {
	if opts.Name == "" {
		return nil, errors.New("forgejo: tags.create: name is required")
	}
	body := struct {
		Name    string `json:"tag_name"`
		Target  string `json:"target,omitempty"`
		Message string `json:"message,omitempty"`
	}{opts.Name, opts.Target, opts.Message}
	var out tagJSON
	if _, err := t.c.api.Do(ctx, http.MethodPost, tagsPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	tag := out.toTag()
	return &tag, nil
}

func (t tags) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := t.c.api.Do(ctx, http.MethodDelete, tagsPath(owner, repo, name), nil, nil, nil)
	return err
}
