package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func toRepo(r *gh.Repository) gitoperationslib.Repo {
	vis := gitoperationslib.Visibility(r.GetVisibility())
	if !vis.Valid() {
		vis = gitoperationslib.VisibilityPublic
		if r.GetPrivate() {
			vis = gitoperationslib.VisibilityPrivate
		}
	}
	return gitoperationslib.Repo{
		ID:            r.GetID(),
		Owner:         r.GetOwner().GetLogin(),
		Name:          r.GetName(),
		FullName:      r.GetFullName(),
		Description:   r.GetDescription(),
		Website:       r.GetHomepage(),
		Visibility:    vis,
		DefaultBranch: r.GetDefaultBranch(),
		Fork:          r.GetFork(),
		Mirror:        r.GetMirrorURL() != "",
		Archived:      r.GetArchived(),
		Template:      r.GetIsTemplate(),
		HTMLURL:       r.GetHTMLURL(),
		CloneURL:      r.GetCloneURL(),
		SSHURL:        r.GetSSHURL(),
		CreatedAt:     r.GetCreatedAt().Time,
		UpdatedAt:     r.GetUpdatedAt().Time,
	}
}

// visibilityFields maps v to the request fields. Public and private use the
// private flag, because the visibility field is ignored when creating a
// repository for a user (it only applies to organisations); internal can
// only be expressed through visibility. Empty means "server default".
func visibilityFields(v gitoperationslib.Visibility, op string) (private *bool, visibility *string, err error) {
	switch v {
	case "":
		return nil, nil, nil
	case gitoperationslib.VisibilityPublic:
		return gh.Ptr(false), nil, nil
	case gitoperationslib.VisibilityPrivate:
		return gh.Ptr(true), nil, nil
	case gitoperationslib.VisibilityInternal:
		return nil, gh.Ptr(string(v)), nil
	}
	return nil, nil, fmt.Errorf("github: %s: invalid visibility %q", op, v)
}

// nonEmpty returns nil for "", so the field is omitted from the request.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type repos struct{ c *Client }

func (r repos) Get(ctx context.Context, owner, name string) (*gitoperationslib.Repo, error) {
	repo, _, err := r.c.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return nil, convertError(err)
	}
	out := toRepo(repo)
	return &out, nil
}

func (r repos) List(ctx context.Context, owner string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	list, resp, err := r.list(ctx, owner, gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Repo]{
		Items:    make([]gitoperationslib.Repo, len(list)),
		NextPage: resp.NextPage,
	}
	for i, repo := range list {
		page.Items[i] = toRepo(repo)
	}
	return page, nil
}

// list picks the list endpoint for owner: everything the authenticated user
// can access, their own repositories, an organisation's, or another user's.
func (r repos) list(ctx context.Context, owner string, lo gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
	if owner == "" {
		return r.c.gh.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{ListOptions: lo})
	}
	me, err := r.c.currentLogin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if strings.EqualFold(owner, me) {
		// The /users endpoint only returns public repositories, even for the
		// authenticated user.
		return r.c.gh.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{Affiliation: "owner", ListOptions: lo})
	}
	org, err := r.c.isOrg(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	if org {
		return r.c.gh.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{ListOptions: lo})
	}
	return r.c.gh.Repositories.ListByUser(ctx, owner, &gh.RepositoryListByUserOptions{ListOptions: lo})
}

func (r repos) Create(ctx context.Context, opts gitoperationslib.CreateRepoOptions) (*gitoperationslib.Repo, error) {
	const op = "repos.create"
	if opts.Name == "" {
		return nil, errors.New("github: repos.create: name is required")
	}
	if opts.DefaultBranch != "" {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "default branch cannot be set on create")
	}
	if len(opts.Gitignores) > 1 {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "at most one gitignore template")
	}
	private, vis, err := visibilityFields(opts.Visibility, op)
	if err != nil {
		return nil, err
	}
	org, err := r.createOrg(ctx, opts.Owner)
	if err != nil {
		return nil, err
	}

	req := gh.CreateRepositoryRequest{
		Name:            opts.Name,
		Description:     nonEmpty(opts.Description),
		Private:         private,
		Visibility:      vis,
		IsTemplate:      gh.Ptr(opts.Template),
		AutoInit:        gh.Ptr(opts.AutoInit),
		LicenseTemplate: nonEmpty(opts.License),
	}
	if len(opts.Gitignores) == 1 {
		req.GitignoreTemplate = &opts.Gitignores[0]
	}
	repo, _, err := r.c.gh.Repositories.Create(ctx, org, req)
	if err != nil {
		return nil, convertError(err)
	}
	out := toRepo(repo)
	return &out, nil
}

// createOrg returns the organisation to create in, or "" for the
// authenticated user. GitHub cannot create repositories for another user.
func (r repos) createOrg(ctx context.Context, owner string) (string, error) {
	if owner == "" {
		return "", nil
	}
	me, err := r.c.currentLogin(ctx)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(owner, me) {
		return "", nil
	}
	org, err := r.c.isOrg(ctx, owner)
	if err != nil {
		return "", err
	}
	if !org {
		return "", gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "repos.create", "cannot create repositories for another user")
	}
	return owner, nil
}

func (r repos) Edit(ctx context.Context, owner, name string, opts gitoperationslib.EditRepoOptions) (*gitoperationslib.Repo, error) {
	body := &gh.Repository{
		Name:          opts.Name,
		Description:   opts.Description,
		Homepage:      opts.Website,
		DefaultBranch: opts.DefaultBranch,
		Archived:      opts.Archived,
		IsTemplate:    opts.Template,
	}
	if opts.Visibility != nil {
		if *opts.Visibility == "" {
			return nil, errors.New(`github: repos.edit: invalid visibility ""`)
		}
		private, vis, err := visibilityFields(*opts.Visibility, "repos.edit")
		if err != nil {
			return nil, err
		}
		body.Private, body.Visibility = private, vis
	}
	repo, _, err := r.c.gh.Repositories.Edit(ctx, owner, name, body)
	if err != nil {
		return nil, convertError(err)
	}
	out := toRepo(repo)
	return &out, nil
}

func (r repos) Delete(ctx context.Context, owner, name string) error {
	_, err := r.c.gh.Repositories.Delete(ctx, owner, name)
	return convertError(err)
}
