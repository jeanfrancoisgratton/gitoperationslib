package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestUserExtraFields(t *testing.T) {
	var j userJSON
	err := json.Unmarshal([]byte(`{"login": "alice", "avatar_url": "https://h/a.png", "pronouns": "she/her",
		"language": "fr-FR", "followers_count": 4, "following_count": 2, "starred_repos_count": 9}`), &j)
	if err != nil {
		t.Fatal(err)
	}
	u := j.toUser()
	if u.AvatarURL != "https://h/a.png" || u.Pronouns != "she/her" || u.Language != "fr-FR" ||
		u.Followers != 4 || u.Following != 2 || u.StarredRepos != 9 {
		t.Errorf("got %+v", u)
	}
	var o orgJSON
	if err := json.Unmarshal([]byte(`{"username": "acme", "avatar_url": "https://h/o.png"}`), &o); err != nil {
		t.Fatal(err)
	}
	if got := o.toOrg().AvatarURL; got != "https://h/o.png" {
		t.Errorf("org avatar = %q", got)
	}
}

// TestDirectoryNonMember checks that parts the caller may not see (a
// non-member listing an organisation's teams) leave the info incomplete
// instead of failing it.
func TestDirectoryNonMember(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs/acme/teams":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"Must be an organization member"}`)
		case "/api/v1/orgs/acme/members":
			io.WriteString(w, "["+userResponse+"]")
		case "/api/v1/orgs/acme/repos":
			io.WriteString(w, "["+repoResponse+"]")
		default:
			http.NotFound(w, r)
		}
	})
	info, err := c.Directory().OrgInfo(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "acme" || len(info.Members) != 1 || len(info.Repos) != 1 || len(info.Teams) != 0 {
		t.Errorf("info = %+v", info)
	}
	if !gitoperationslib.IsForbidden(info.Incomplete) {
		t.Errorf("Incomplete = %v, want the 403 on teams", info.Incomplete)
	}
	want := []string{"GET /api/v1/orgs/acme/teams", "GET /api/v1/orgs/acme/members", "GET /api/v1/orgs/acme/repos"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}
}

func TestDirectoryRequests(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/v1/users/alice":
			io.WriteString(w, userResponse)
		case "/api/v1/users/alice/orgs":
			io.WriteString(w, "["+orgResponse+"]")
		case "/api/v1/users/alice/repos":
			io.WriteString(w, "["+repoResponse+"]")
		case "/api/v1/orgs/acme/teams/search":
			io.WriteString(w, `{"ok": true, "data": [`+teamResponse+`]}`)
		case "/api/v1/teams/6/members":
			io.WriteString(w, "["+userResponse+"]")
		case "/api/v1/teams/6/repos":
			io.WriteString(w, "["+repoResponse+"]")
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	dir := c.Directory()

	user, err := dir.UserInfo(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "alice" || len(user.Orgs) != 1 || user.Orgs[0].Name != "acme" || len(user.Repos) != 1 || user.Incomplete != nil {
		t.Errorf("UserInfo = %+v", user)
	}

	team, err := dir.TeamInfo(ctx, "acme", "DEVS")
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != 6 || team.Slug != "devs" || team.Org != "acme" || len(team.Members) != 1 || len(team.Repos) != 1 || team.Incomplete != nil {
		t.Errorf("TeamInfo = %+v", team)
	}

	members, err := dir.TeamMembers(ctx, "acme", "devs", gitoperationslib.ListOptions{Page: 2, PerPage: 5})
	if err != nil || len(members.Items) != 1 {
		t.Fatalf("TeamMembers = %+v, %v", members, err)
	}
	if last := requests[len(requests)-1]; last != "GET /api/v1/teams/6/members?limit=5&page=2" {
		t.Errorf("last request = %s", last)
	}

	// The main object missing fails the call.
	if _, err := dir.TeamInfo(ctx, "acme", "nobody"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown team: err = %v, want 404", err)
	}
	if _, err := dir.UserInfo(ctx, "ghost"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown user: err = %v, want 404", err)
	}
}

func TestDirectoryServerError(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/alice" {
			io.WriteString(w, userResponse)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := c.Directory().UserInfo(context.Background(), "alice")
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("err = %v, want the 500 (server errors are not tolerated)", err)
	}
}
