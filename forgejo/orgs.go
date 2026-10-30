package forgejo

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/refusal"
)

type orgJSON struct {
	ID                        int64  `json:"id"`
	Username                  string `json:"username"`
	Name                      string `json:"name"`
	FullName                  string `json:"full_name"`
	Description               string `json:"description"`
	Website                   string `json:"website"`
	Location                  string `json:"location"`
	Email                     string `json:"email"`
	Visibility                string `json:"visibility"`
	AvatarURL                 string `json:"avatar_url"`
	RepoAdminChangeTeamAccess bool   `json:"repo_admin_change_team_access"`
}

func (o *orgJSON) toOrg() gitoperationslib.Org {
	name := o.Username
	if name == "" {
		name = o.Name
	}
	return gitoperationslib.Org{
		ID:                        o.ID,
		Name:                      name,
		FullName:                  o.FullName,
		Description:               o.Description,
		Website:                   o.Website,
		Location:                  o.Location,
		Email:                     o.Email,
		Visibility:                gitoperationslib.OwnerVisibility(o.Visibility),
		AvatarURL:                 o.AvatarURL,
		RepoAdminChangeTeamAccess: o.RepoAdminChangeTeamAccess,
	}
}

type createOrgBody struct {
	Username                  string `json:"username"`
	FullName                  string `json:"full_name,omitempty"`
	Description               string `json:"description,omitempty"`
	Website                   string `json:"website,omitempty"`
	Location                  string `json:"location,omitempty"`
	Email                     string `json:"email,omitempty"`
	Visibility                string `json:"visibility,omitempty"`
	RepoAdminChangeTeamAccess bool   `json:"repo_admin_change_team_access,omitempty"`
}

type editOrgBody struct {
	FullName                  *string `json:"full_name,omitempty"`
	Description               *string `json:"description,omitempty"`
	Website                   *string `json:"website,omitempty"`
	Location                  *string `json:"location,omitempty"`
	Email                     *string `json:"email,omitempty"`
	Visibility                *string `json:"visibility,omitempty"`
	RepoAdminChangeTeamAccess *bool   `json:"repo_admin_change_team_access,omitempty"`
}

// Orgs implements gitoperationslib.Admin.
func (c *Client) Orgs() gitoperationslib.Orgs { return orgs{c} }

type orgs struct{ c *Client }

func (o orgs) Get(ctx context.Context, name string) (*gitoperationslib.Org, error) {
	var out orgJSON
	if _, err := o.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", name), nil, nil, &out); err != nil {
		return nil, err
	}
	org := out.toOrg()
	return &org, nil
}

func (o orgs) List(ctx context.Context, user string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	path := "user/orgs"
	if user != "" {
		path = forgejoapi.Path("users", user, "orgs")
	}
	return o.list(ctx, path, opts)
}

func (o orgs) ListAll(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	return o.list(ctx, "orgs", opts)
}

func (o orgs) list(ctx context.Context, path string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	var out []orgJSON
	resp, err := o.c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Org]{}, err
	}
	return toPage(resp, out, (*orgJSON).toOrg), nil
}

func (o orgs) Create(ctx context.Context, opts gitoperationslib.CreateOrgOptions) (*gitoperationslib.Org, error) {
	const op = "orgs.create"
	if opts.Name == "" {
		return nil, errors.New("forgejo: orgs.create: name is required")
	}
	vis, err := ownerVisibility(opts.Visibility, op, true)
	if err != nil {
		return nil, err
	}
	path := "orgs"
	if opts.Owner != "" {
		me, err := o.c.currentLogin(ctx)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(opts.Owner, me) {
			path = forgejoapi.Path("admin", "users", opts.Owner, "orgs")
		}
	}
	body := createOrgBody{
		Username:                  opts.Name,
		FullName:                  opts.FullName,
		Description:               opts.Description,
		Website:                   opts.Website,
		Location:                  opts.Location,
		Email:                     opts.Email,
		Visibility:                vis,
		RepoAdminChangeTeamAccess: opts.RepoAdminChangeTeamAccess,
	}
	var out orgJSON
	if _, err := o.c.api.Do(ctx, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	org := out.toOrg()
	return &org, nil
}

func (o orgs) Edit(ctx context.Context, name string, opts gitoperationslib.EditOrgOptions) (*gitoperationslib.Org, error) {
	body := editOrgBody{
		FullName:                  opts.FullName,
		Description:               opts.Description,
		Website:                   opts.Website,
		Location:                  opts.Location,
		Email:                     opts.Email,
		RepoAdminChangeTeamAccess: opts.RepoAdminChangeTeamAccess,
	}
	if opts.Visibility != nil {
		vis, err := ownerVisibility(*opts.Visibility, "orgs.edit", false)
		if err != nil {
			return nil, err
		}
		body.Visibility = &vis
	}
	var out orgJSON
	if _, err := o.c.api.Do(ctx, http.MethodPatch, forgejoapi.Path("orgs", name), nil, body, &out); err != nil {
		return nil, err
	}
	org := out.toOrg()
	return &org, nil
}

func (o orgs) Rename(ctx context.Context, name, newName string) error {
	if newName == "" {
		return errors.New("forgejo: orgs.rename: new name is required")
	}
	body := struct {
		NewName string `json:"new_name"`
	}{newName}
	_, err := o.c.api.Do(ctx, http.MethodPost, forgejoapi.Path("orgs", name, "rename"), nil, body, nil)
	return err
}

func (o orgs) Delete(ctx context.Context, name string) error {
	_, err := o.c.api.Do(ctx, http.MethodDelete, forgejoapi.Path("orgs", name), nil, nil, nil)
	return refusal.Mark(err, refusal.Rule{Status: http.StatusInternalServerError, Fragment: "still has ownership of repositories", Sentinel: gitoperationslib.ErrOwnsRepos})
}

func (o orgs) Members(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	var out []userJSON
	resp, err := o.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", org, "members"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, err
	}
	return toPage(resp, out, (*userJSON).toUser), nil
}

func (o orgs) IsMember(ctx context.Context, org, user string) (bool, error) {
	// Non-members of org are redirected to the public-members check, which
	// answers the same way (204 or 404).
	_, err := o.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", org, "members", user), nil, nil, nil)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (o orgs) RemoveMember(ctx context.Context, org, user string) error {
	_, err := o.c.api.Do(ctx, http.MethodDelete, forgejoapi.Path("orgs", org, "members", user), nil, nil, nil)
	return refusal.Mark(err, refusal.Rule{Status: http.StatusInternalServerError, Fragment: "last member of owner team", Sentinel: gitoperationslib.ErrLastOwner})
}
