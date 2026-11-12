package forgejo

import (
	"context"
	"net/http"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

// The Info reads below work with a regular (non-admin) token; the server
// filters what the caller may see.

func (svc users) Info(ctx context.Context, login string) (*gitoperationslib.UserInfo, error) {
	u, err := svc.Get(ctx, login)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.UserInfo{User: *u}
	var res partial.Result
	info.Orgs, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return orgs{svc.c}.List(ctx, u.Login, o)
	})
	if err := res.Add("organisations", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, repoLister(svc.c, forgejoapi.Path("users", u.Login, "repos")))
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete, info.Missing = res.Err(), res.Parts()
	return info, nil
}

func (o orgs) Info(ctx context.Context, name string) (*gitoperationslib.OrgInfo, error) {
	org, err := o.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.OrgInfo{Org: *org}
	var res partial.Result
	info.Teams, err = partial.Collect(ctx, func(ctx context.Context, lo gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
		return teams{o.c}.List(ctx, org.Name, lo)
	})
	if err := res.Add("teams", err); err != nil {
		return nil, err
	}
	info.Members, err = partial.Collect(ctx, func(ctx context.Context, lo gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return o.Members(ctx, org.Name, lo)
	})
	if err := res.Add("members", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, repoLister(o.c, forgejoapi.Path("orgs", org.Name, "repos")))
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete, info.Missing = res.Err(), res.Parts()
	return info, nil
}

func (t teams) Info(ctx context.Context, org, name string) (*gitoperationslib.TeamInfo, error) {
	team, err := t.Get(ctx, org, name)
	if err != nil {
		return nil, err
	}
	info := &gitoperationslib.TeamInfo{Team: *team}
	var res partial.Result
	info.Members, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return t.membersByID(ctx, team.ID, o)
	})
	if err := res.Add("members", err); err != nil {
		return nil, err
	}
	info.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return t.reposByID(ctx, team.ID, o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	info.Incomplete, info.Missing = res.Err(), res.Parts()
	return info, nil
}

// repoLister lists the repositories at path ("users/u/repos", "orgs/o/repos").
func repoLister(c *Client, path string) func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	return func(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		var out []repoJSON
		resp, err := c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, err
		}
		return toPage(resp, out, (*repoJSON).toRepo), nil
	}
}
