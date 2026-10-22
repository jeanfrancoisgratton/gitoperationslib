package github

import (
	"context"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

func toUser(u *gh.User) gitoperationslib.User {
	return gitoperationslib.User{
		ID:          u.GetID(),
		Login:       u.GetLogin(),
		FullName:    u.GetName(),
		Email:       u.GetEmail(),
		Description: u.GetBio(),
		Website:     u.GetBlog(),
		Location:    u.GetLocation(),
		Visibility:  gitoperationslib.OwnerVisibilityPublic, // GitHub profiles are always public
		IsAdmin:     u.GetSiteAdmin(),
		Active:      u.SuspendedAt == nil,
		HTMLURL:     u.GetHTMLURL(),
		AvatarURL:   u.GetAvatarURL(),
		Company:     u.GetCompany(),
		Followers:   u.GetFollowers(),
		Following:   u.GetFollowing(),
		CreatedAt:   u.GetCreatedAt().Time,
	}
}

func toOrg(o *gh.Organization) gitoperationslib.Org {
	return gitoperationslib.Org{
		ID:          o.GetID(),
		Name:        o.GetLogin(),
		FullName:    o.GetName(),
		Description: o.GetDescription(),
		Website:     o.GetBlog(),
		Location:    o.GetLocation(),
		Email:       o.GetEmail(),
		Visibility:  gitoperationslib.OwnerVisibilityPublic, // GitHub organisations are always public
		AvatarURL:   o.GetAvatarURL(),
		HTMLURL:     o.GetHTMLURL(),
	}
}

// teamPermissions maps a team's default repository permission.
var teamPermissions = map[string]gitoperationslib.TeamPermission{
	"pull": gitoperationslib.TeamPermissionRead, "triage": gitoperationslib.TeamPermissionRead,
	"push": gitoperationslib.TeamPermissionWrite, "maintain": gitoperationslib.TeamPermissionWrite,
	"admin": gitoperationslib.TeamPermissionAdmin,
}

func toTeam(t *gh.Team, org string) gitoperationslib.Team {
	if login := t.GetOrganization().GetLogin(); login != "" {
		org = login
	}
	return gitoperationslib.Team{
		ID:          t.GetID(),
		Org:         org,
		Name:        t.GetName(),
		Slug:        t.GetSlug(),
		Description: t.GetDescription(),
		Permission:  teamPermissions[t.GetPermission()],
	}
}

func toPage[J, T any](list []J, resp *gh.Response, convert func(J) T) gitoperationslib.Page[T] {
	page := gitoperationslib.Page[T]{Items: make([]T, len(list)), NextPage: resp.NextPage}
	for i, item := range list {
		page.Items[i] = convert(item)
	}
	return page
}

func ghList(o gitoperationslib.ListOptions) gh.ListOptions {
	return gh.ListOptions{Page: o.Page, PerPage: o.PerPage}
}

// Directory implements gitoperationslib.Client. Every call works with a
// regular token; GitHub filters what the caller may see (non-members only
// see an organisation's public members and visible teams).
func (c *Client) Directory() gitoperationslib.Directory { return directory{c} }

type directory struct{ c *Client }

func (d directory) User(ctx context.Context, login string) (*gitoperationslib.User, error) {
	u, _, err := d.c.gh.Users.Get(ctx, login)
	if err != nil {
		return nil, convertError(err)
	}
	out := toUser(u)
	return &out, nil
}

func (d directory) UserInfo(ctx context.Context, login string) (*gitoperationslib.UserInfo, error) {
	u, err := d.User(ctx, login)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.UserInfo{User: *u}
	var res partial.Result
	info.Orgs, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return d.ListOrgs(ctx, u.Login, o)
	})
	if err := res.Add("organisations", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return repos{d.c}.List(ctx, u.Login, o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) Org(ctx context.Context, name string) (*gitoperationslib.Org, error) {
	o, _, err := d.c.gh.Organizations.Get(ctx, name)
	if err != nil {
		return nil, convertError(err)
	}
	out := toOrg(o)
	return &out, nil
}

func (d directory) ListOrgs(ctx context.Context, user string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	lo := ghList(opts)
	list, resp, err := d.c.gh.Organizations.List(ctx, user, &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Org]{}, convertError(err)
	}
	return toPage(list, resp, toOrg), nil
}

// ListAllOrgs pages with GitHub's "since" cursor (an organisation ID), which
// travels in Page and NextPage.
func (d directory) ListAllOrgs(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	list, resp, err := d.c.gh.Organizations.ListAll(ctx, &gh.OrganizationsListOptions{Since: int64(opts.Page), PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Org]{}, convertError(err)
	}
	return toPage(list, resp, toOrg), nil
}

func (d directory) OrgInfo(ctx context.Context, name string) (*gitoperationslib.OrgInfo, error) {
	org, err := d.Org(ctx, name)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.OrgInfo{Org: *org}
	var res partial.Result
	info.Teams, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
		return d.ListTeams(ctx, org.Name, o)
	})
	if err := res.Add("teams", err); err != nil {
		return nil, err
	}
	info.Members, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return d.OrgMembers(ctx, org.Name, o)
	})
	if err := res.Add("members", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		lo := gh.RepositoryListByOrgOptions{ListOptions: ghList(o)}
		list, resp, err := d.c.gh.Repositories.ListByOrg(ctx, org.Name, &lo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, convertError(err)
		}
		return toPage(list, resp, toRepo), nil
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) OrgMembers(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	list, resp, err := d.c.gh.Organizations.ListMembers(ctx, org, &gh.ListMembersOptions{ListOptions: ghList(opts)})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, convertError(err)
	}
	return toPage(list, resp, toUser), nil
}

func (d directory) ListTeams(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
	lo := ghList(opts)
	list, resp, err := d.c.gh.Teams.ListTeams(ctx, org, &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Team]{}, convertError(err)
	}
	return toPage(list, resp, func(t *gh.Team) gitoperationslib.Team { return toTeam(t, org) }), nil
}

func (d directory) Team(ctx context.Context, org, name string) (*gitoperationslib.Team, error) {
	t, _, err := d.c.gh.Teams.GetTeamBySlug(ctx, org, name)
	if err != nil {
		return nil, convertError(err)
	}
	out := toTeam(t, org)
	return &out, nil
}

func (d directory) TeamInfo(ctx context.Context, org, name string) (*gitoperationslib.TeamInfo, error) {
	team, err := d.Team(ctx, org, name)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.TeamInfo{Team: *team}
	var res partial.Result
	info.Members, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return d.TeamMembers(ctx, org, team.Slug, o)
	})
	if err := res.Add("members", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return d.TeamRepos(ctx, org, team.Slug, o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) TeamMembers(ctx context.Context, org, name string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	list, resp, err := d.c.gh.Teams.ListTeamMembersBySlug(ctx, org, name, &gh.TeamListTeamMembersOptions{ListOptions: ghList(opts)})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, convertError(err)
	}
	return toPage(list, resp, toUser), nil
}

func (d directory) TeamRepos(ctx context.Context, org, name string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	lo := ghList(opts)
	list, resp, err := d.c.gh.Teams.ListTeamReposBySlug(ctx, org, name, &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, convertError(err)
	}
	return toPage(list, resp, toRepo), nil
}
