package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

type linkJSON struct {
	Href string `json:"href"`
	Name string `json:"name"`
}

type repoJSON struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Website     string `json:"website"`
	IsPrivate   bool   `json:"is_private"`
	MainBranch  *struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	Parent *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
	Workspace struct {
		Slug string `json:"slug"`
	} `json:"workspace"`
	Links struct {
		HTML  linkJSON   `json:"html"`
		Clone []linkJSON `json:"clone"`
	} `json:"links"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
}

func (r *repoJSON) toRepo() gitoperationslib.Repo {
	repo := gitoperationslib.Repo{
		Owner:       r.Workspace.Slug,
		Name:        r.Slug,
		FullName:    r.FullName,
		Description: r.Description,
		Website:     r.Website,
		Visibility:  gitoperationslib.VisibilityPublic,
		Fork:        r.Parent != nil,
		HTMLURL:     r.Links.HTML.Href,
		CreatedAt:   r.CreatedOn,
		UpdatedAt:   r.UpdatedOn,
	}
	if repo.Owner == "" {
		repo.Owner, _, _ = strings.Cut(r.FullName, "/")
	}
	if r.IsPrivate {
		repo.Visibility = gitoperationslib.VisibilityPrivate
	}
	if r.MainBranch != nil {
		repo.DefaultBranch = r.MainBranch.Name
	}
	for _, l := range r.Links.Clone {
		switch l.Name {
		case "https":
			repo.CloneURL = l.Href
		case "ssh":
			repo.SSHURL = l.Href
		}
	}
	return repo
}

func repoPath(workspace, slug string, rest ...string) string {
	return bitbucketapi.Path(append([]string{"repositories", workspace, slug}, rest...)...)
}

// Repos implements gitoperationslib.Client.
func (c *Client) Repos() gitoperationslib.Repos { return repos{c} }

type repos struct{ c *Client }

func (r repos) Get(ctx context.Context, owner, name string) (*gitoperationslib.Repo, error) {
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, repoPath(owner, name), nil, nil, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

// List returns a workspace's repositories. Empty owner lists the
// repositories of every workspace the user belongs to (Bitbucket retired its
// cross-workspace listing): paged normally with one workspace, as a single
// page with several.
func (r repos) List(ctx context.Context, owner string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	if owner == "" {
		workspaces, err := r.c.workspaces(ctx)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, err
		}
		if len(workspaces) == 1 {
			return r.List(ctx, workspaces[0], opts)
		}
		var all gitoperationslib.Page[gitoperationslib.Repo]
		for _, ws := range workspaces {
			repos, err := bitbucketapi.All[repoJSON](ctx, r.c.api, bitbucketapi.Path("repositories", ws), nil)
			if err != nil {
				return gitoperationslib.Page[gitoperationslib.Repo]{}, err
			}
			for i := range repos {
				all.Items = append(all.Items, repos[i].toRepo())
			}
		}
		return all, nil
	}
	page, err := bitbucketapi.List[repoJSON](ctx, r.c.api, bitbucketapi.Path("repositories", owner), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Repo]{Items: make([]gitoperationslib.Repo, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toRepo()
	}
	return out, nil
}

func isPrivate(v gitoperationslib.Visibility, op string) (*bool, error) {
	switch v {
	case "":
		return nil, nil
	case gitoperationslib.VisibilityPublic:
		return gitoperationslib.Ptr(false), nil
	case gitoperationslib.VisibilityPrivate:
		return gitoperationslib.Ptr(true), nil
	case gitoperationslib.VisibilityInternal:
		return nil, notSupported(op, "internal visibility")
	}
	return nil, fmt.Errorf("bitbucket: %s: invalid visibility %q", op, v)
}

// Create creates a repository in the workspace Owner (empty: the user's only
// workspace), in the workspace's default project. Bitbucket creates empty
// repositories; AutoInit commits a README.md.
func (r repos) Create(ctx context.Context, opts gitoperationslib.CreateRepoOptions) (*gitoperationslib.Repo, error) {
	const op = "repos.create"
	if opts.Name == "" {
		return nil, errNoName
	}
	switch {
	case len(opts.Gitignores) > 0:
		return nil, notSupported(op, "gitignore templates")
	case opts.License != "":
		return nil, notSupported(op, "license templates")
	case opts.Template:
		return nil, notSupported(op, "template repositories")
	case opts.DefaultBranch != "" && !opts.AutoInit:
		return nil, notSupported(op, "a default branch without AutoInit (empty repositories have no branches)")
	}
	private, err := isPrivate(opts.Visibility, op)
	if err != nil {
		return nil, err
	}
	workspace := opts.Owner
	if workspace == "" {
		if workspace, err = r.c.defaultWorkspace(ctx); err != nil {
			return nil, err
		}
	}
	slug := strings.ToLower(opts.Name)
	body := struct {
		SCM         string `json:"scm"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		IsPrivate   *bool  `json:"is_private,omitempty"`
	}{"git", opts.Name, opts.Description, private}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPost, repoPath(workspace, slug), nil, body, &out); err != nil {
		return nil, err
	}
	if opts.AutoInit {
		form := url.Values{
			"README.md": {"# " + opts.Name + "\n\n" + opts.Description + "\n"},
			"message":   {"Initial commit"},
		}
		if opts.DefaultBranch != "" {
			form.Set("branch", opts.DefaultBranch)
		}
		if _, err := r.c.api.PostForm(ctx, repoPath(workspace, out.Slug, "src"), form, nil); err != nil {
			return nil, fmt.Errorf("bitbucket: %s: created %s but the initial commit failed: %w", op, out.FullName, err)
		}
		return r.Get(ctx, workspace, out.Slug)
	}
	repo := out.toRepo()
	return &repo, nil
}

// Edit changes a repository. Renaming also changes its slug (and URLs).
func (r repos) Edit(ctx context.Context, owner, name string, opts gitoperationslib.EditRepoOptions) (*gitoperationslib.Repo, error) {
	const op = "repos.edit"
	if opts.Archived != nil {
		return nil, notSupported(op, "archiving")
	}
	if opts.Template != nil {
		return nil, notSupported(op, "template repositories")
	}
	body := map[string]any{}
	if opts.Name != nil {
		body["name"] = *opts.Name
	}
	if opts.Description != nil {
		body["description"] = *opts.Description
	}
	if opts.Website != nil {
		body["website"] = *opts.Website
	}
	if opts.DefaultBranch != nil {
		body["mainbranch"] = map[string]string{"name": *opts.DefaultBranch}
	}
	if opts.Visibility != nil {
		private, err := isPrivate(*opts.Visibility, op)
		if err != nil {
			return nil, err
		}
		if private != nil {
			body["is_private"] = *private
		}
	}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPut, repoPath(owner, name), nil, body, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

func (r repos) Delete(ctx context.Context, owner, name string) error {
	_, err := r.c.api.Do(ctx, http.MethodDelete, repoPath(owner, name), nil, nil, nil)
	return err
}
