package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type repoJSON struct {
	ID    int64 `json:"id"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Website       string    `json:"website"`
	Private       bool      `json:"private"`
	Internal      bool      `json:"internal"`
	DefaultBranch string    `json:"default_branch"`
	Fork          bool      `json:"fork"`
	Mirror        bool      `json:"mirror"`
	Archived      bool      `json:"archived"`
	Template      bool      `json:"template"`
	Empty         bool      `json:"empty"`
	HTMLURL       string    `json:"html_url"`
	CloneURL      string    `json:"clone_url"`
	SSHURL        string    `json:"ssh_url"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (r *repoJSON) toRepo() gitoperationslib.Repo {
	vis := gitoperationslib.VisibilityPublic
	switch {
	case r.Private:
		vis = gitoperationslib.VisibilityPrivate
	case r.Internal:
		vis = gitoperationslib.VisibilityInternal
	}
	return gitoperationslib.Repo{
		ID:            r.ID,
		Owner:         r.Owner.Login,
		Name:          r.Name,
		FullName:      r.FullName,
		Description:   r.Description,
		Website:       r.Website,
		Visibility:    vis,
		DefaultBranch: r.DefaultBranch,
		Fork:          r.Fork,
		Mirror:        r.Mirror,
		Archived:      r.Archived,
		Template:      r.Template,
		Empty:         r.Empty,
		HTMLURL:       r.HTMLURL,
		CloneURL:      r.CloneURL,
		SSHURL:        r.SSHURL,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

type createRepoBody struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Private       *bool  `json:"private,omitempty"`
	AutoInit      bool   `json:"auto_init,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Template      bool   `json:"template,omitempty"`
	Gitignores    string `json:"gitignores,omitempty"`
	License       string `json:"license,omitempty"`
}

type editRepoBody struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Website       *string `json:"website,omitempty"`
	Private       *bool   `json:"private,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
	Archived      *bool   `json:"archived,omitempty"`
	Template      *bool   `json:"template,omitempty"`
}

// privateFlag maps a Visibility to Forgejo's private flag; nil means
// "server default". Forgejo cannot set internal visibility on a repository.
func privateFlag(v gitoperationslib.Visibility, op string) (*bool, error) {
	switch v {
	case "":
		return nil, nil
	case gitoperationslib.VisibilityPublic:
		return gitoperationslib.Ptr(false), nil
	case gitoperationslib.VisibilityPrivate:
		return gitoperationslib.Ptr(true), nil
	case gitoperationslib.VisibilityInternal:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "internal visibility")
	}
	return nil, fmt.Errorf("forgejo: %s: invalid visibility %q", op, v)
}

type repos struct{ c *Client }

func (r repos) Get(ctx context.Context, owner, name string) (*gitoperationslib.Repo, error) {
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, name), nil, nil, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

func (r repos) List(ctx context.Context, owner string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	path := "user/repos"
	if owner != "" {
		org, err := r.c.isOrg(ctx, owner)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, err
		}
		if org {
			path = forgejoapi.Path("orgs", owner, "repos")
		} else {
			path = forgejoapi.Path("users", owner, "repos")
		}
	}

	var out []repoJSON
	resp, err := r.c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, err
	}
	page := gitoperationslib.Page[gitoperationslib.Repo]{
		Items:    make([]gitoperationslib.Repo, len(out)),
		NextPage: resp.NextPage,
	}
	for i := range out {
		page.Items[i] = out[i].toRepo()
	}
	return page, nil
}

func (r repos) Create(ctx context.Context, opts gitoperationslib.CreateRepoOptions) (*gitoperationslib.Repo, error) {
	if opts.Name == "" {
		return nil, errors.New("forgejo: repos.create: name is required")
	}
	private, err := privateFlag(opts.Visibility, "repos.create")
	if err != nil {
		return nil, err
	}
	path, err := r.createPath(ctx, opts.Owner)
	if err != nil {
		return nil, err
	}
	body := createRepoBody{
		Name:          opts.Name,
		Description:   opts.Description,
		Private:       private,
		AutoInit:      opts.AutoInit,
		DefaultBranch: opts.DefaultBranch,
		Template:      opts.Template,
		Gitignores:    strings.Join(opts.Gitignores, ","),
		License:       opts.License,
	}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

// createPath picks the create endpoint for owner: the authenticated user, an
// organisation, or (admin only) another user.
func (r repos) createPath(ctx context.Context, owner string) (string, error) {
	if owner == "" {
		return "user/repos", nil
	}
	me, err := r.c.currentLogin(ctx)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(owner, me) {
		return "user/repos", nil
	}
	org, err := r.c.isOrg(ctx, owner)
	if err != nil {
		return "", err
	}
	if org {
		return forgejoapi.Path("orgs", owner, "repos"), nil
	}
	return forgejoapi.Path("admin", "users", owner, "repos"), nil
}

func (r repos) Edit(ctx context.Context, owner, name string, opts gitoperationslib.EditRepoOptions) (*gitoperationslib.Repo, error) {
	body := editRepoBody{
		Name:          opts.Name,
		Description:   opts.Description,
		Website:       opts.Website,
		DefaultBranch: opts.DefaultBranch,
		Archived:      opts.Archived,
		Template:      opts.Template,
	}
	if opts.Visibility != nil {
		private, err := privateFlag(*opts.Visibility, "repos.edit")
		if err != nil {
			return nil, err
		}
		body.Private = private
	}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPatch, forgejoapi.Path("repos", owner, name), nil, body, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

func (r repos) Delete(ctx context.Context, owner, name string) error {
	_, err := r.c.api.Do(ctx, http.MethodDelete, forgejoapi.Path("repos", owner, name), nil, nil, nil)
	return err
}
