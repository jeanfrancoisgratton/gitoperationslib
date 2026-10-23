package contracttest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Directory runs the read-only user, organisation and team checks. It
// creates nothing.
func Directory(t *testing.T, env Env) {
	ctx := testContext(t)
	dir := env.Client.Directory()

	// Every backend implements Me.
	me, err := dir.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(me.Login, env.Login) {
		t.Errorf("Me().Login = %q, want %s", me.Login, env.Login)
	}
	if me.Incomplete != nil {
		t.Logf("Me incomplete: %v", me.Incomplete)
	}
	t.Logf("me %s: %d e-mail addresses, %d organisations, %d teams, %d repositories, %d SSH keys, %d GPG keys",
		me.Login, len(me.Emails), len(me.Orgs), len(me.Teams), len(me.Repos), len(me.SSHKeys), len(me.GPGKeys))

	if unsupported(t, env, gitoperationslib.CapDirectory, func() error {
		_, err := dir.User(ctx, env.Login)
		return err
	}) {
		return
	}

	user, err := dir.UserInfo(ctx, env.Login)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(user.Login, env.Login) || user.ID == 0 || user.HTMLURL == "" {
		t.Errorf("UserInfo = %+v", user.User)
	}
	if user.Incomplete != nil {
		t.Errorf("own UserInfo incomplete: %v", user.Incomplete)
	}
	t.Logf("user %s: %d organisations, %d repositories", user.Login, len(user.Orgs), len(user.Repos))

	org, err := dir.Org(ctx, env.Org)
	if gitoperationslib.IsNotFound(err) {
		t.Skipf("%s is not an organisation; skipping organisation and team checks", env.Org)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(org.Name, env.Org) {
		t.Errorf("Org = %+v", org)
	}

	// The test user owns env.Org, so the organisation is among its own.
	var mine []string
	for o, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 50},
		func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
			return dir.ListOrgs(ctx, "", o)
		}) {
		if err != nil {
			t.Fatal(err)
		}
		mine = append(mine, strings.ToLower(o.Name))
	}
	if !slices.Contains(mine, strings.ToLower(env.Org)) {
		t.Errorf("ListOrgs(\"\") = %v, missing %s", mine, env.Org)
	}

	info, err := dir.OrgInfo(ctx, env.Org)
	if err != nil {
		t.Fatal(err)
	}
	if info.Incomplete != nil {
		t.Errorf("OrgInfo of an owned organisation incomplete: %v", info.Incomplete)
	}
	if !slices.ContainsFunc(info.Members, func(u gitoperationslib.User) bool { return strings.EqualFold(u.Login, env.Login) }) {
		t.Errorf("OrgInfo members lack %s: %d members", env.Login, len(info.Members))
	}
	t.Logf("org %s: %d teams, %d members, %d repositories", info.Name, len(info.Teams), len(info.Members), len(info.Repos))

	if len(info.Teams) == 0 {
		t.Logf("%s has no teams; skipping team checks", env.Org)
		return
	}
	first := info.Teams[0]
	team, err := dir.TeamInfo(ctx, env.Org, first.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != first.ID || team.Name != first.Name || !strings.EqualFold(team.Org, env.Org) || team.Incomplete != nil {
		t.Errorf("TeamInfo(%s) = %+v", first.Slug, team)
	}
	members, err := dir.TeamMembers(ctx, env.Org, first.Slug, gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members.Items {
		if !slices.ContainsFunc(team.Members, func(u gitoperationslib.User) bool { return u.Login == m.Login }) {
			t.Errorf("TeamMembers returned %s, absent from TeamInfo", m.Login)
		}
	}
}
