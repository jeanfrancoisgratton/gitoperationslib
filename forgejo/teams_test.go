package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const (
	teamResponse = `{
		"id": 6, "name": "devs", "description": "d", "organization": {"username": "acme"},
		"permission": "read", "units_map": {"repo.code": "write", "repo.issues": "read"},
		"includes_all_repositories": false, "can_create_org_repo": true
	}`
	ownerTeamResponse = `{"id": 5, "name": "Owners", "organization": {"username": "acme"}, "permission": "owner", "units_map": {"repo.code": "owner"}}`
)

func TestTeamConversion(t *testing.T) {
	var j teamJSON
	if err := json.Unmarshal([]byte(teamResponse), &j); err != nil {
		t.Fatal(err)
	}
	team := j.toTeam()
	if team.ID != 6 || team.Org != "acme" || team.Name != "devs" || team.Description != "d" ||
		team.Permission != gitoperationslib.TeamPermissionRead || !team.CanCreateOrgRepo || team.IncludesAllRepos ||
		team.Units[gitoperationslib.TeamUnitCode] != gitoperationslib.TeamPermissionWrite ||
		team.Units[gitoperationslib.TeamUnitIssues] != gitoperationslib.TeamPermissionRead || len(team.Units) != 2 {
		t.Errorf("got %+v", team)
	}
}

func TestTeamSetAccess(t *testing.T) {
	allWrite := map[string]string{}
	for _, u := range gitoperationslib.AllTeamUnits {
		allWrite[string(u)] = "write"
	}
	tests := []struct {
		name      string
		access    gitoperationslib.TeamAccess
		wantPerm  string
		wantUnits map[string]string
		wantErr   bool
	}{
		{"admin", gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionAdmin}, "admin", nil, false},
		{"units", gitoperationslib.TeamAccess{Units: map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{
			gitoperationslib.TeamUnitCode: gitoperationslib.TeamPermissionWrite, gitoperationslib.TeamUnitWiki: gitoperationslib.TeamPermissionNone,
		}}, "read", map[string]string{"repo.code": "write", "repo.wiki": "none"}, false},
		{"write shorthand", gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionWrite}, "read", allWrite, false},
		{"admin with units", gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionAdmin, Units: map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{gitoperationslib.TeamUnitCode: "read"}}, "", nil, true},
		{"both", gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionRead, Units: map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{gitoperationslib.TeamUnitCode: "read"}}, "", nil, true},
		{"admin unit", gitoperationslib.TeamAccess{Units: map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{gitoperationslib.TeamUnitCode: "admin"}}, "", nil, true},
		{"owner", gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionOwner}, "", nil, true},
		{"empty", gitoperationslib.TeamAccess{}, "", nil, true},
	}
	for _, tt := range tests {
		var b teamBody
		err := b.setAccess(tt.access, "test")
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if err == nil && (b.Permission != tt.wantPerm || fmt.Sprint(b.UnitsMap) != fmt.Sprint(tt.wantUnits)) {
			t.Errorf("%s: permission=%q units=%v; want %q %v", tt.name, b.Permission, b.UnitsMap, tt.wantPerm, tt.wantUnits)
		}
	}
}

func TestTeamsRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodPut, r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/orgs/acme/teams" && r.Method == http.MethodGet:
			io.WriteString(w, "["+teamResponse+"]")
		case r.URL.Path == "/api/v1/teams/6/members":
			io.WriteString(w, "["+userResponse+"]")
		case r.URL.Path == "/api/v1/teams/6/repos":
			io.WriteString(w, "["+repoResponse+"]")
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, teamResponse)
		default:
			io.WriteString(w, teamResponse)
		}
	})
	ctx := context.Background()
	teams := c.Teams()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	page := gitoperationslib.ListOptions{PerPage: 5}

	list, err := teams.List(ctx, "acme", page)
	check(err)
	if len(list.Items) != 1 || list.Items[0].Name != "devs" || list.Items[0].Org != "acme" {
		t.Errorf("List = %+v", list)
	}
	_, err = teams.Get(ctx, 6)
	check(err)
	_, err = teams.Create(ctx, "acme", gitoperationslib.CreateTeamOptions{Name: "devs", Access: gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionAdmin}})
	check(err)
	_, err = teams.Edit(ctx, 6, gitoperationslib.EditTeamOptions{Description: gitoperationslib.Ptr("x")})
	check(err)
	members, err := teams.Members(ctx, 6, page)
	check(err)
	if len(members.Items) != 1 || members.Items[0].Login != "alice" {
		t.Errorf("Members = %+v", members)
	}
	ok, err := teams.IsMember(ctx, 6, "alice")
	check(err)
	if !ok {
		t.Error("IsMember = false")
	}
	check(teams.AddMember(ctx, 6, "alice"))
	check(teams.RemoveMember(ctx, 6, "alice"))
	repos, err := teams.Repos(ctx, 6, page)
	check(err)
	if len(repos.Items) != 1 || repos.Items[0].Name != "widget" {
		t.Errorf("Repos = %+v", repos)
	}
	check(teams.AddRepo(ctx, 6, "acme", "widget"))
	check(teams.RemoveRepo(ctx, 6, "acme", "widget"))
	check(teams.Delete(ctx, 6))

	wantRequests := []string{
		"GET /api/v1/orgs/acme/teams?limit=5",
		"GET /api/v1/teams/6?",
		"POST /api/v1/orgs/acme/teams?",
		"GET /api/v1/teams/6?",
		"PATCH /api/v1/teams/6?",
		"GET /api/v1/teams/6/members?limit=5",
		"GET /api/v1/teams/6/members/alice?",
		"PUT /api/v1/teams/6/members/alice?",
		"DELETE /api/v1/teams/6/members/alice?",
		"GET /api/v1/teams/6/repos?limit=5",
		"PUT /api/v1/teams/6/repos/acme/widget?",
		"DELETE /api/v1/teams/6/repos/acme/widget?",
		"GET /api/v1/teams/6?",
		"DELETE /api/v1/teams/6?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		2: {"name": "devs", "description": "", "permission": "admin", "includes_all_repositories": false, "can_create_org_repo": false},
		4: {"name": "devs", "description": "x"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestOwnerTeamGuard(t *testing.T) {
	var writes []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method)
		}
		io.WriteString(w, ownerTeamResponse)
	})
	ctx := context.Background()
	teams := c.Teams()

	if err := teams.Delete(ctx, 5); !errors.Is(err, gitoperationslib.ErrOwnerTeam) {
		t.Errorf("Delete: err = %v, want ErrOwnerTeam", err)
	}
	if _, err := teams.Edit(ctx, 5, gitoperationslib.EditTeamOptions{Name: gitoperationslib.Ptr("Renamed")}); !errors.Is(err, gitoperationslib.ErrOwnerTeam) {
		t.Errorf("rename: err = %v, want ErrOwnerTeam", err)
	}
	if _, err := teams.Edit(ctx, 5, gitoperationslib.EditTeamOptions{Access: &gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionRead}}); !errors.Is(err, gitoperationslib.ErrOwnerTeam) {
		t.Errorf("access change: err = %v, want ErrOwnerTeam", err)
	}
	if len(writes) != 0 {
		t.Fatalf("guarded operations sent %v", writes)
	}
	// Changing only the description, or "renaming" to the same name, is allowed.
	if _, err := teams.Edit(ctx, 5, gitoperationslib.EditTeamOptions{Name: gitoperationslib.Ptr("Owners"), Description: gitoperationslib.Ptr("x")}); err != nil {
		t.Errorf("description edit: %v", err)
	}
}

func TestTeamGetByName(t *testing.T) {
	var queries []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		// Page 1 has only a partial match; page 2 has the exact one.
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<http://h/api/v1/orgs/acme/teams/search?page=2>; rel="next"`)
			io.WriteString(w, `{"ok":true,"data":[{"id":1,"name":"devs-old"}]}`)
			return
		}
		io.WriteString(w, `{"ok":true,"data":[{"id":6,"name":"Devs"}]}`)
	})
	team, err := c.Teams().GetByName(context.Background(), "acme", "devs")
	if err != nil || team.ID != 6 || team.Org != "acme" {
		t.Fatalf("GetByName = %+v, %v", team, err)
	}
	if len(queries) != 2 || queries[0] != "limit=50&page=1&q=devs" {
		t.Errorf("queries = %v", queries)
	}

	if _, err := c.Teams().GetByName(context.Background(), "acme", "nobody"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("missing team: err = %v, want 404", err)
	}
}

func TestTeamsRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	if _, err := c.Teams().Create(ctx, "acme", gitoperationslib.CreateTeamOptions{Access: gitoperationslib.TeamAccess{Permission: "read"}}); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := c.Teams().Create(ctx, "acme", gitoperationslib.CreateTeamOptions{Name: "x"}); err == nil {
		t.Error("missing access accepted")
	}
}
