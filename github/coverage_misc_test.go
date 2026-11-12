package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestDirectoryUserAndTeamGitHub(t *testing.T) {
	var requests []string
	teamMembers := true
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/users/alice":
			io.WriteString(w, `{"login":"alice","name":"Alice"}`)
		case "/api/v3/users/alice/orgs":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"Forbidden"}`)
		case "/api/v3/users/alice/repos":
			io.WriteString(w, "["+repoResponse+"]")
		case "/api/v3/orgs/acme/teams/back-end":
			io.WriteString(w, teamResponse)
		case "/api/v3/orgs/acme/teams/back-end/members":
			if !teamMembers {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"message":"Forbidden"}`)
				return
			}
			io.WriteString(w, `[{"login":"bob"}]`)
		case "/api/v3/orgs/acme/teams/back-end/repos":
			io.WriteString(w, "["+repoResponse+"]")
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	})
	ctx := context.Background()

	// A part the token may not read is reported, not fatal.
	user, err := c.Users().Info(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "alice" || len(user.Orgs) != 0 || len(user.Repos) != 1 || !gitoperationslib.IsForbidden(user.Incomplete) {
		t.Errorf("UserInfo = %+v", user)
	}
	if len(user.Missing) != 1 || user.Missing[0].Part != "organisations" {
		t.Errorf("Missing = %+v", user.Missing)
	}
	// The main object missing fails the call.
	if _, err := c.Users().Info(ctx, "nobody"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("UserInfo(nobody): err = %v, want 404", err)
	}

	team, err := c.Teams().Info(ctx, "acme", "back-end")
	if err != nil {
		t.Fatal(err)
	}
	if team.Slug != "back-end" || len(team.Members) != 1 || team.Members[0].Login != "bob" || len(team.Repos) != 1 || team.Incomplete != nil {
		t.Errorf("TeamInfo = %+v", team)
	}
	teamMembers = false
	if team, err = c.Teams().Info(ctx, "acme", "back-end"); err != nil || !gitoperationslib.IsForbidden(team.Incomplete) || len(team.Repos) != 1 {
		t.Errorf("partial TeamInfo = %+v, %v", team, err)
	}
	if _, err := c.Teams().Info(ctx, "acme", "nobody"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("TeamInfo(nobody): err = %v, want 404", err)
	}
	if _, err := c.Teams().Members(ctx, "acme", "nobody", gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("TeamMembers(nobody): err = %v, want 404", err)
	}
	if _, err := c.Teams().Repos(ctx, "acme", "nobody", gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("TeamRepos(nobody): err = %v, want 404", err)
	}
}

func TestRenameBranchGitHub(t *testing.T) {
	var renames []string
	var bodies []map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v3/repos/acme/widget/branches/main/rename":
			renames = append(renames, r.URL.Path)
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"trunk"}`)
		case "GET /api/v3/repos/acme/widget/branches/trunk":
			io.WriteString(w, `{"name":"trunk","commit":{"sha":"abc"},"protected":false}`)
		case "GET /api/v3/repos/acme/widget":
			io.WriteString(w, `{"name":"widget","default_branch":"trunk"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	br, err := c.Branches().Rename(ctx, "acme", "widget", "main", "trunk")
	if err != nil || br.Name != "trunk" || br.SHA != "abc" {
		t.Fatalf("Rename = %+v, %v", br, err)
	}
	if bodies[0]["new_name"] != "trunk" {
		t.Errorf("body = %v", bodies[0])
	}
	if _, err := c.Branches().Rename(ctx, "acme", "widget", "main", ""); err == nil {
		t.Error("empty new name accepted")
	}
	if len(renames) != 1 {
		t.Errorf("renames = %v", renames)
	}
}

func TestListTagsGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "5" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Link", `<https://x/?page=2>; rel="next"`)
		io.WriteString(w, `[{"name":"v1","commit":{"sha":"abc"},"tarball_url":"https://t","zipball_url":"https://z"}]`)
	})
	page, err := c.Tags().List(context.Background(), "acme", "widget", gitoperationslib.ListOptions{PerPage: 5})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.Name != "v1" || got.SHA != "abc" || got.TarballURL != "https://t" || got.ZipballURL != "https://z" {
		t.Errorf("tag = %+v", got)
	}
}

func TestPullGetAndRemoveReviewersGitHub(t *testing.T) {
	var body map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/repos/acme/widget/pulls/7":
			io.WriteString(w, `{"number":7,"title":"T","state":"open","head":{"ref":"topic","sha":"abc"},"base":{"ref":"main"}}`)
		case "DELETE /api/v3/repos/acme/widget/pulls/7/requested_reviewers":
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	pr, err := c.PullRequests().Get(ctx, "acme", "widget", 7)
	if err != nil || pr.Number != 7 || pr.Head.Ref != "topic" || pr.Base.Ref != "main" {
		t.Errorf("Get = %+v, %v", pr, err)
	}
	if err := c.PullRequests().RemoveReviewers(ctx, "acme", "widget", 7, []string{"bob"}, []string{"devs"}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(body) != "map[reviewers:[bob] team_reviewers:[devs]]" {
		t.Errorf("body = %v", body)
	}
}

func TestTagRulesetListAndDeleteGitHub(t *testing.T) {
	c, store := rulesetServer(t)
	ctx := context.Background()
	tp := c.TagProtections()
	for _, pattern := range []string{"v*", "release-*"} {
		if _, err := tp.Create(ctx, "acme", "widget", gitoperationslib.TagProtection{Pattern: pattern, Allowlist: gitoperationslib.Allowlist{Teams: []string{"devs"}}}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := tp.List(ctx, "acme", "widget")
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := tp.Delete(ctx, "acme", "widget", "v*"); err != nil {
		t.Fatal(err)
	}
	if len(store) != 1 {
		t.Errorf("after Delete: %d rulesets left, want 1", len(store))
	}
	for _, rs := range store {
		if fmt.Sprint(rs["conditions"]) != "map[ref_name:map[exclude:[] include:[refs/tags/release-*]]]" {
			t.Errorf("the wrong ruleset was deleted: %v", rs)
		}
	}
	if err := tp.Delete(ctx, "acme", "widget", "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown pattern: err = %v, want 404", err)
	}
}

func TestRepoVariableSetGitHub(t *testing.T) {
	var requests []string
	status := http.StatusNoContent
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(status)
		if status >= 400 {
			fmt.Fprintf(w, `{"message":"%s"}`, http.StatusText(status))
		}
	})
	ctx := context.Background()
	repo := gitoperationslib.RepoCI("acme", "widget")
	const patch = "PATCH /api/v3/repos/acme/widget/actions/variables/ENV"
	const post = "POST /api/v3/repos/acme/widget/actions/variables"

	if err := c.CI().Variables().Set(ctx, repo, "ENV", "v"); err != nil || !slices.Equal(requests, []string{patch}) {
		t.Errorf("existing: %v, requests %v", err, requests)
	}
	requests, status = nil, http.StatusNotFound
	if err := c.CI().Variables().Set(ctx, repo, "ENV", "v"); err != nil || !slices.Equal(requests, []string{patch, post}) {
		t.Errorf("new: %v, requests %v", err, requests)
	}
	// Only a 404 means "create it".
	requests, status = nil, http.StatusForbidden
	if err := c.CI().Variables().Set(ctx, repo, "ENV", "v"); !gitoperationslib.IsForbidden(err) || !slices.Equal(requests, []string{patch}) {
		t.Errorf("forbidden: %v, requests %v", err, requests)
	}
	requests = nil
	if err := c.CI().Variables().Set(ctx, repo, "", "v"); err == nil {
		t.Error("empty name accepted")
	}
	if err := c.CI().Variables().Set(ctx, gitoperationslib.UserCI(), "ENV", "v"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user scope: err = %v, want ErrNotSupported", err)
	}
	if len(requests) != 0 {
		t.Errorf("refused calls sent %v", requests)
	}
}

func TestContentType(t *testing.T) {
	for _, tc := range []struct{ name, given, want string }{
		{"a.txt", "text/x-custom", "text/x-custom"},
		{"a.json", "", "application/json"},
		{"a.nosuchextension", "", "application/octet-stream"},
		{"noextension", "", "application/octet-stream"},
	} {
		if got := contentType(tc.name, tc.given); got != tc.want {
			t.Errorf("contentType(%q, %q) = %q, want %q", tc.name, tc.given, got, tc.want)
		}
	}
}

func TestAvatarNotSupportedGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL) })
	if err := c.Account().Profile().DeleteAvatar(context.Background()); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("err = %v", err)
	}
}

func TestUserAgentGitHub(t *testing.T) {
	var agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		io.WriteString(w, `{"name":"widget"}`)
	}))
	t.Cleanup(srv.Close)
	c, err := New("pat", WithEnterpriseURL(srv.URL, ""), WithHTTPClient(srv.Client()), WithUserAgent("gitclt/1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Repos().Get(context.Background(), "acme", "widget"); err != nil {
		t.Fatal(err)
	}
	if agent != "gitclt/1" {
		t.Errorf("User-Agent = %q", agent)
	}
}
