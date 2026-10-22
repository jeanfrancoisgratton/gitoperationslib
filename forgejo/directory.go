package forgejo

import (
	"context"
	"net/http"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

// Directory implements gitoperationslib.Client. Every call works with a
// regular (non-admin) token; the server filters what the caller may see.
func (c *Client) Directory() gitoperationslib.Directory { return directory{c} }

type directory struct{ c *Client }

func (d directory) User(ctx context.Context, login string) (*gitoperationslib.User, error) {
	return users{d.c}.Get(ctx, login)
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
	info.Repos, err = partial.Collect(ctx, d.repoLister(forgejoapi.Path("users", u.Login, "repos")))
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) Org(ctx context.Context, name string) (*gitoperationslib.Org, error) {
	return orgs{d.c}.Get(ctx, name)
}

func (d directory) ListOrgs(ctx context.Context, user string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	return orgs{d.c}.List(ctx, user, opts)
}

func (d directory) ListAllOrgs(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
	return orgs{d.c}.ListAll(ctx, opts)
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
	info.Repos, err = partial.Collect(ctx, d.repoLister(forgejoapi.Path("orgs", org.Name, "repos")))
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) OrgMembers(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	return orgs{d.c}.Members(ctx, org, opts)
}

func (d directory) ListTeams(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
	return teams{d.c}.List(ctx, org, opts)
}

func (d directory) Team(ctx context.Context, org, name string) (*gitoperationslib.Team, error) {
	return teams{d.c}.GetByName(ctx, org, name)
}

func (d directory) TeamInfo(ctx context.Context, org, name string) (*gitoperationslib.TeamInfo, error) {
	team, err := d.Team(ctx, org, name)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.TeamInfo{Team: *team}
	var res partial.Result
	info.Members, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return teams{d.c}.Members(ctx, team.ID, o)
	})
	if err := res.Add("members", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return teams{d.c}.Repos(ctx, team.ID, o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete = res.Err()
	return info, nil
}

func (d directory) TeamMembers(ctx context.Context, org, name string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	team, err := d.Team(ctx, org, name)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, err
	}
	return teams{d.c}.Members(ctx, team.ID, opts)
}

func (d directory) TeamRepos(ctx context.Context, org, name string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	team, err := d.Team(ctx, org, name)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, err
	}
	return teams{d.c}.Repos(ctx, team.ID, opts)
}

// repoLister lists the repositories at path ("users/u/repos", "orgs/o/repos").
func (d directory) repoLister(path string) func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	return func(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		var out []repoJSON
		resp, err := d.c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, err
		}
		return toPage(resp, out, (*repoJSON).toRepo), nil
	}
}
