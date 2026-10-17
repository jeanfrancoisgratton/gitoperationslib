package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const orgResponse = `{
	"id": 8, "name": "acme", "username": "acme", "full_name": "Acme Inc.", "email": "o@acme",
	"description": "d", "website": "https://acme", "location": "Québec", "visibility": "limited",
	"repo_admin_change_team_access": true
}`

func TestOrgConversion(t *testing.T) {
	var j orgJSON
	if err := json.Unmarshal([]byte(orgResponse), &j); err != nil {
		t.Fatal(err)
	}
	o := j.toOrg()
	want := gitoperationslib.Org{
		ID: 8, Name: "acme", FullName: "Acme Inc.", Email: "o@acme", Description: "d", Website: "https://acme",
		Location: "Québec", Visibility: gitoperationslib.OwnerVisibilityLimited, RepoAdminChangeTeamAccess: true,
	}
	if o != want {
		t.Errorf("got  %+v\nwant %+v", o, want)
	}
	if got := (&orgJSON{Name: "legacy"}).toOrg().Name; got != "legacy" {
		t.Errorf("fallback to name: got %q", got)
	}
}

func TestOrgsRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete, r.URL.Path == "/api/v1/orgs/acme/rename",
			r.URL.Path == "/api/v1/orgs/acme/members/alice":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/orgs/acme/members":
			io.WriteString(w, "["+userResponse+"]")
		case r.Method == http.MethodGet && r.URL.Path != "/api/v1/orgs/acme":
			io.WriteString(w, "["+orgResponse+"]")
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, orgResponse)
		default:
			io.WriteString(w, orgResponse)
		}
	})
	ctx := context.Background()
	orgs := c.Orgs()
	page := gitoperationslib.ListOptions{PerPage: 5}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// GET /orgs/acme is answered by fakeForgejo itself, so it is not recorded.
	o, err := orgs.Get(ctx, "acme")
	check(err)
	if o.Name != "acme" {
		t.Errorf("Get = %+v", o)
	}
	_, err = orgs.List(ctx, "", page)
	check(err)
	_, err = orgs.List(ctx, "bob", page)
	check(err)
	all, err := orgs.ListAll(ctx, page)
	check(err)
	if len(all.Items) != 1 || all.Items[0].Name != "acme" {
		t.Errorf("ListAll = %+v", all)
	}
	_, err = orgs.Create(ctx, gitoperationslib.CreateOrgOptions{Name: "acme", Visibility: gitoperationslib.OwnerVisibilityPrivate})
	check(err)
	_, err = orgs.Create(ctx, gitoperationslib.CreateOrgOptions{Name: "acme", Owner: "me"})
	check(err)
	_, err = orgs.Create(ctx, gitoperationslib.CreateOrgOptions{Name: "acme", Owner: "bob", FullName: "Acme"})
	check(err)
	vis := gitoperationslib.OwnerVisibilityPublic
	_, err = orgs.Edit(ctx, "acme", gitoperationslib.EditOrgOptions{Description: gitoperationslib.Ptr(""), Visibility: &vis})
	check(err)
	check(orgs.Rename(ctx, "acme", "acme2"))
	members, err := orgs.Members(ctx, "acme", page)
	check(err)
	if len(members.Items) != 1 || members.Items[0].Login != "alice" {
		t.Errorf("Members = %+v", members)
	}
	isMember, err := orgs.IsMember(ctx, "acme", "alice")
	check(err)
	if !isMember {
		t.Error("IsMember(alice) = false")
	}
	check(orgs.RemoveMember(ctx, "acme", "alice"))
	check(orgs.Delete(ctx, "acme2"))

	wantRequests := []string{
		"GET /api/v1/user/orgs?limit=5",
		"GET /api/v1/users/bob/orgs?limit=5",
		"GET /api/v1/orgs?limit=5",
		"POST /api/v1/orgs?",
		"POST /api/v1/orgs?",
		"POST /api/v1/admin/users/bob/orgs?",
		"PATCH /api/v1/orgs/acme?",
		"POST /api/v1/orgs/acme/rename?",
		"GET /api/v1/orgs/acme/members?limit=5",
		"GET /api/v1/orgs/acme/members/alice?",
		"DELETE /api/v1/orgs/acme/members/alice?",
		"DELETE /api/v1/orgs/acme2?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		3: {"username": "acme", "visibility": "private"},
		5: {"username": "acme", "full_name": "Acme"},
		6: {"description": "", "visibility": "public"},
		7: {"new_name": "acme2"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestIsMemberNotFound(t *testing.T) {
	c := fakeForgejo(t, http.NotFound)
	if ok, err := c.Orgs().IsMember(context.Background(), "acme", "nobody"); ok || err != nil {
		t.Errorf("IsMember = %v, %v; want false, nil", ok, err)
	}
}

func TestOrgsRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{}); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: "x", Visibility: "internal"}); err == nil {
		t.Error("invalid visibility accepted")
	}
	if err := c.Orgs().Rename(ctx, "x", ""); err == nil {
		t.Error("empty new name accepted")
	}
}
