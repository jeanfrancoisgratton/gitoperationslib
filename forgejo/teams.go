package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type teamJSON struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Organization *struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"organization"`
	Permission       string            `json:"permission"`
	UnitsMap         map[string]string `json:"units_map"`
	IncludesAllRepos bool              `json:"includes_all_repositories"`
	CanCreateOrgRepo bool              `json:"can_create_org_repo"`
}

func (t *teamJSON) toTeam() gitoperationslib.Team {
	team := gitoperationslib.Team{
		ID:               t.ID,
		Name:             t.Name,
		Slug:             t.Name,
		Description:      t.Description,
		Permission:       gitoperationslib.TeamPermission(t.Permission),
		Units:            make(map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission, len(t.UnitsMap)),
		IncludesAllRepos: t.IncludesAllRepos,
		CanCreateOrgRepo: t.CanCreateOrgRepo,
	}
	if t.Organization != nil {
		team.Org = t.Organization.Username
		if team.Org == "" {
			team.Org = t.Organization.Name
		}
	}
	for unit, perm := range t.UnitsMap {
		team.Units[gitoperationslib.TeamUnit(unit)] = gitoperationslib.TeamPermission(perm)
	}
	return team
}

// teamBody is used for both create and edit.
type teamBody struct {
	Name             *string           `json:"name,omitempty"`
	Description      *string           `json:"description,omitempty"`
	Permission       string            `json:"permission,omitempty"`
	UnitsMap         map[string]string `json:"units_map,omitempty"`
	IncludesAllRepos *bool             `json:"includes_all_repositories,omitempty"`
	CanCreateOrgRepo *bool             `json:"can_create_org_repo,omitempty"`
}

// setAccess fills the permission fields from a TeamAccess. Forgejo applies
// "admin" to every unit and ignores units_map with it; with any other
// permission, units_map wins and the overall level is derived from it. A
// permission alone is rejected by the server, so read/write shorthand is
// expanded to every unit.
func (b *teamBody) setAccess(a gitoperationslib.TeamAccess, op string) error {
	switch {
	case a.Permission == gitoperationslib.TeamPermissionAdmin:
		if len(a.Units) > 0 {
			return fmt.Errorf("forgejo: %s: admin permission applies to every unit; leave Units empty", op)
		}
		b.Permission = string(a.Permission)
		return nil
	case len(a.Units) > 0:
		if a.Permission != "" {
			return fmt.Errorf("forgejo: %s: set either Permission or Units, not both", op)
		}
		b.UnitsMap = make(map[string]string, len(a.Units))
		for unit, perm := range a.Units {
			switch perm {
			case gitoperationslib.TeamPermissionNone, gitoperationslib.TeamPermissionRead, gitoperationslib.TeamPermissionWrite:
			default:
				return fmt.Errorf("forgejo: %s: invalid permission %q for unit %s", op, perm, unit)
			}
			b.UnitsMap[string(unit)] = string(perm)
		}
	case a.Permission == gitoperationslib.TeamPermissionRead || a.Permission == gitoperationslib.TeamPermissionWrite:
		b.UnitsMap = make(map[string]string, len(gitoperationslib.AllTeamUnits))
		for _, unit := range gitoperationslib.AllTeamUnits {
			b.UnitsMap[string(unit)] = string(a.Permission)
		}
	default:
		return fmt.Errorf("forgejo: %s: invalid team access %+v", op, a)
	}
	// Any non-admin level makes the server honour units_map, including when
	// lowering a team from admin.
	b.Permission = string(gitoperationslib.TeamPermissionRead)
	return nil
}

func teamPath(id int64, rest ...string) string {
	return forgejoapi.Path(append([]string{"teams", strconv.FormatInt(id, 10)}, rest...)...)
}

// Teams implements gitoperationslib.Admin.
func (c *Client) Teams() gitoperationslib.Teams { return teams{c} }

type teams struct{ c *Client }

func (t teams) List(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
	var out []teamJSON
	resp, err := t.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", org, "teams"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Team]{}, err
	}
	// List and search responses omit the organisation; fill it in.
	page := toPage(resp, out, (*teamJSON).toTeam)
	for i := range page.Items {
		page.Items[i].Org = org
	}
	return page, nil
}

func (t teams) Get(ctx context.Context, id int64) (*gitoperationslib.Team, error) {
	var out teamJSON
	if _, err := t.c.api.Do(ctx, http.MethodGet, teamPath(id), nil, nil, &out); err != nil {
		return nil, err
	}
	team := out.toTeam()
	return &team, nil
}

func (t teams) GetByName(ctx context.Context, org, name string) (*gitoperationslib.Team, error) {
	q := url.Values{"q": {name}, "limit": {"50"}}
	for page := 1; ; page++ {
		q.Set("page", strconv.Itoa(page))
		var out struct {
			Data []teamJSON `json:"data"`
		}
		resp, err := t.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", org, "teams", "search"), q, nil, &out)
		if err != nil {
			return nil, err
		}
		for i := range out.Data {
			if strings.EqualFold(out.Data[i].Name, name) {
				team := out.Data[i].toTeam()
				team.Org = org
				return &team, nil
			}
		}
		if resp.NextPage == 0 || len(out.Data) == 0 {
			break
		}
	}
	return nil, &gitoperationslib.APIError{
		Backend:    gitoperationslib.BackendForgejo,
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("team %q not found in %s", name, org),
	}
}

func (t teams) Create(ctx context.Context, org string, opts gitoperationslib.CreateTeamOptions) (*gitoperationslib.Team, error) {
	const op = "teams.create"
	if opts.Name == "" {
		return nil, errors.New("forgejo: teams.create: name is required")
	}
	body := teamBody{
		Name:             &opts.Name,
		Description:      &opts.Description,
		IncludesAllRepos: &opts.IncludesAllRepos,
		CanCreateOrgRepo: &opts.CanCreateOrgRepo,
	}
	if err := body.setAccess(opts.Access, op); err != nil {
		return nil, err
	}
	var out teamJSON
	if _, err := t.c.api.Do(ctx, http.MethodPost, forgejoapi.Path("orgs", org, "teams"), nil, body, &out); err != nil {
		return nil, err
	}
	team := out.toTeam()
	return &team, nil
}

func (t teams) Edit(ctx context.Context, id int64, opts gitoperationslib.EditTeamOptions) (*gitoperationslib.Team, error) {
	const op = "teams.edit"
	cur, err := t.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	renaming := opts.Name != nil && *opts.Name != cur.Name
	if cur.Permission == gitoperationslib.TeamPermissionOwner && (renaming || opts.Access != nil) {
		return nil, fmt.Errorf("forgejo: %s: team %q of %s: %w", op, cur.Name, cur.Org, gitoperationslib.ErrOwnerTeam)
	}
	body := teamBody{
		// The swagger marks name as required on edit; send the current one.
		Name:             &cur.Name,
		Description:      opts.Description,
		IncludesAllRepos: opts.IncludesAllRepos,
		CanCreateOrgRepo: opts.CanCreateOrgRepo,
	}
	if opts.Name != nil {
		body.Name = opts.Name
	}
	if opts.Access != nil {
		if err := body.setAccess(*opts.Access, op); err != nil {
			return nil, err
		}
	}
	var out teamJSON
	if _, err := t.c.api.Do(ctx, http.MethodPatch, teamPath(id), nil, body, &out); err != nil {
		return nil, err
	}
	team := out.toTeam()
	return &team, nil
}

func (t teams) Delete(ctx context.Context, id int64) error {
	cur, err := t.Get(ctx, id)
	if err != nil {
		return err
	}
	if cur.Permission == gitoperationslib.TeamPermissionOwner {
		return fmt.Errorf("forgejo: teams.delete: team %q of %s: %w", cur.Name, cur.Org, gitoperationslib.ErrOwnerTeam)
	}
	_, err = t.c.api.Do(ctx, http.MethodDelete, teamPath(id), nil, nil, nil)
	return err
}

func (t teams) Members(ctx context.Context, id int64, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	var out []userJSON
	resp, err := t.c.api.Do(ctx, http.MethodGet, teamPath(id, "members"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, err
	}
	return toPage(resp, out, (*userJSON).toUser), nil
}

func (t teams) IsMember(ctx context.Context, id int64, user string) (bool, error) {
	_, err := t.c.api.Do(ctx, http.MethodGet, teamPath(id, "members", user), nil, nil, nil)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (t teams) AddMember(ctx context.Context, id int64, user string) error {
	_, err := t.c.api.Do(ctx, http.MethodPut, teamPath(id, "members", user), nil, nil, nil)
	return err
}

func (t teams) RemoveMember(ctx context.Context, id int64, user string) error {
	_, err := t.c.api.Do(ctx, http.MethodDelete, teamPath(id, "members", user), nil, nil, nil)
	return err
}

func (t teams) Repos(ctx context.Context, id int64, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	var out []repoJSON
	resp, err := t.c.api.Do(ctx, http.MethodGet, teamPath(id, "repos"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, err
	}
	return toPage(resp, out, (*repoJSON).toRepo), nil
}

func (t teams) AddRepo(ctx context.Context, id int64, owner, repo string) error {
	_, err := t.c.api.Do(ctx, http.MethodPut, teamPath(id, "repos", owner, repo), nil, nil, nil)
	return err
}

func (t teams) RemoveRepo(ctx context.Context, id int64, owner, repo string) error {
	_, err := t.c.api.Do(ctx, http.MethodDelete, teamPath(id, "repos", owner, repo), nil, nil, nil)
	return err
}
