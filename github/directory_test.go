package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const (
	userResponse = `{"id": 3, "login": "alice", "name": "Alice", "email": "a@example.com", "bio": "b",
		"blog": "https://a", "location": "Montréal", "company": "Acme", "site_admin": false,
		"html_url": "https://github.com/alice", "avatar_url": "https://avatars/a", "followers": 4, "following": 2,
		"created_at": "2026-10-17T10:00:00Z"}`
	orgResponse = `{"id": 8, "login": "acme", "name": "Acme Inc.", "description": "d", "blog": "https://acme",
		"location": "Québec", "email": "o@acme", "avatar_url": "https://avatars/o", "html_url": "https://github.com/acme"}`
	teamResponse = `{"id": 6, "name": "Back End", "slug": "back-end", "description": "d", "permission": "push",
		"organization": {"login": "acme"}}`
)

func TestDirectoryConversions(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/users/alice":
			io.WriteString(w, userResponse)
		case "/api/v3/orgs/acme/teams/back-end":
			io.WriteString(w, teamResponse)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	u, err := c.Directory().User(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	want := gitoperationslib.User{
		ID: 3, Login: "alice", FullName: "Alice", Email: "a@example.com", Description: "b", Website: "https://a",
		Location: "Montréal", Visibility: gitoperationslib.OwnerVisibilityPublic, Active: true,
		HTMLURL: "https://github.com/alice", AvatarURL: "https://avatars/a", Company: "Acme", Followers: 4, Following: 2,
		CreatedAt: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC),
	}
	if *u != want {
		t.Errorf("User:\ngot  %+v\nwant %+v", *u, want)
	}

	team, err := c.Directory().Team(ctx, "acme", "back-end")
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != 6 || team.Name != "Back End" || team.Slug != "back-end" || team.Org != "acme" || team.Permission != gitoperationslib.TeamPermissionWrite {
		t.Errorf("Team = %+v", team)
	}
}

func TestDirectoryOrgInfo(t *testing.T) {
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/v3/orgs/acme/teams":
			// The second page links nowhere; the first links to it.
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", fmt.Sprintf(`<%s?page=2>; rel="next"`, r.URL.Path))
			}
			io.WriteString(w, "["+teamResponse+"]")
		case "/api/v3/orgs/acme/members":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"Forbidden"}`)
		case "/api/v3/orgs/acme/repos":
			io.WriteString(w, "["+repoResponse+"]")
		default:
			http.NotFound(w, r)
		}
	})
	// fakeGitHub answers /orgs/acme with a bare login.
	info, err := c.Directory().OrgInfo(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "acme" || len(info.Teams) != 2 || info.Teams[0].Org != "acme" || len(info.Repos) != 1 || len(info.Members) != 0 {
		t.Errorf("OrgInfo = %+v", info)
	}
	if !gitoperationslib.IsForbidden(info.Incomplete) {
		t.Errorf("Incomplete = %v, want the 403 on members", info.Incomplete)
	}
	if len(requests) != 4 {
		t.Errorf("requests = %v", requests)
	}
}

func TestDirectoryListAllOrgsCursor(t *testing.T) {
	var since []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/organizations" {
			http.NotFound(w, r)
			return
		}
		since = append(since, r.URL.Query().Get("since"))
		if r.URL.Query().Get("since") == "" {
			w.Header().Set("Link", `<https://api.github.com/organizations?since=8>; rel="next"`)
		}
		io.WriteString(w, "["+orgResponse+"]")
	})
	var names []string
	for org, err := range gitoperationslib.All(context.Background(), gitoperationslib.ListOptions{},
		func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
			return c.Directory().ListAllOrgs(ctx, o)
		}) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, org.Name)
	}
	if len(names) != 2 || len(since) != 2 || since[0] != "" || since[1] != "8" {
		t.Errorf("names = %v, since = %v", names, since)
	}
}
