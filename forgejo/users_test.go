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

const userResponse = `{
	"id": 3, "login": "alice", "full_name": "Alice", "email": "a@example.com",
	"description": "d", "website": "https://a", "location": "Montréal", "visibility": "limited",
	"is_admin": true, "active": true, "restricted": false, "prohibit_login": false,
	"html_url": "https://h/alice", "created": "2026-10-17T10:00:00Z", "last_login": "1970-01-01T00:00:00Z"
}`

func TestUserConversion(t *testing.T) {
	var j userJSON
	if err := json.Unmarshal([]byte(userResponse), &j); err != nil {
		t.Fatal(err)
	}
	u := j.toUser()
	if u.ID != 3 || u.Login != "alice" || u.FullName != "Alice" || u.Email != "a@example.com" ||
		u.Location != "Montréal" || u.Visibility != gitoperationslib.OwnerVisibilityLimited ||
		!u.IsAdmin || !u.Active || u.HTMLURL != "https://h/alice" || u.CreatedAt.Hour() != 10 {
		t.Errorf("got %+v", u)
	}
	if !u.LastLogin.IsZero() {
		t.Errorf("LastLogin = %v, want zero for the epoch", u.LastLogin)
	}
}

func TestUsersRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete || r.URL.Path == "/api/v1/admin/users/alice/rename":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			io.WriteString(w, "["+userResponse+"]")
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, userResponse)
		default:
			io.WriteString(w, userResponse)
		}
	})
	ctx := context.Background()
	users := c.Users()

	if u, err := users.Get(ctx, "alice"); err != nil || u.Login != "alice" {
		t.Fatalf("Get = %+v, %v", u, err)
	}
	if p, err := users.List(ctx, gitoperationslib.ListOptions{Page: 2, PerPage: 10}); err != nil || len(p.Items) != 1 {
		t.Fatalf("List = %+v, %v", p, err)
	}
	if _, err := users.Create(ctx, gitoperationslib.CreateUserOptions{
		Login: "alice", Email: "a@example.com", Password: "pw", MustChangePassword: gitoperationslib.Ptr(false),
		Visibility: gitoperationslib.OwnerVisibilityPrivate,
	}); err != nil {
		t.Fatal(err)
	}
	vis := gitoperationslib.OwnerVisibilityLimited // matches userResponse
	if _, err := users.Edit(ctx, "alice", gitoperationslib.EditUserOptions{
		FullName: gitoperationslib.Ptr(""), Visibility: &vis, Admin: gitoperationslib.Ptr(false), MaxRepoCreation: gitoperationslib.Ptr(-1),
	}); err != nil {
		t.Fatal(err)
	}
	if err := users.Rename(ctx, "alice", "alicia"); err != nil {
		t.Fatal(err)
	}
	if err := users.Delete(ctx, "alicia", gitoperationslib.DeleteUserOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := users.Delete(ctx, "alicia", gitoperationslib.DeleteUserOptions{Purge: true}); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{
		"GET /api/v1/users/alice?",
		"GET /api/v1/admin/users?limit=10&page=2",
		"POST /api/v1/admin/users?",
		"PATCH /api/v1/admin/users/alice?",
		"POST /api/v1/admin/users/alice/rename?",
		"DELETE /api/v1/admin/users/alicia?",
		"DELETE /api/v1/admin/users/alicia?purge=true",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		2: {"username": "alice", "email": "a@example.com", "password": "pw", "must_change_password": false, "visibility": "private"},
		3: {"full_name": "", "visibility": "limited", "admin": false, "max_repo_creation": -1.0},
		4: {"new_username": "alicia"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestUserEditVisibilityIgnored(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, userResponse) // visibility stays "limited"
	})
	public := gitoperationslib.OwnerVisibilityPublic
	u, err := c.Users().Edit(context.Background(), "alice", gitoperationslib.EditUserOptions{Visibility: &public})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) || u == nil || u.Login != "alice" {
		t.Errorf("Edit = %+v, %v; want user and ErrNotSupported", u, err)
	}
	limited := gitoperationslib.OwnerVisibilityLimited
	if _, err := c.Users().Edit(context.Background(), "alice", gitoperationslib.EditUserOptions{Visibility: &limited}); err != nil {
		t.Errorf("matching visibility: err = %v", err)
	}
}

func TestUsersRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{Login: "x"}); err == nil {
		t.Error("missing email accepted")
	}
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{Login: "x", Email: "x@e", Visibility: "secret"}); err == nil {
		t.Error("invalid visibility accepted")
	}
	empty := gitoperationslib.OwnerVisibility("")
	if _, err := c.Users().Edit(ctx, "x", gitoperationslib.EditUserOptions{Visibility: &empty}); err == nil {
		t.Error("empty visibility accepted on edit")
	}
	if err := c.Users().Rename(ctx, "x", ""); err == nil {
		t.Error("empty new login accepted")
	}
}

func TestAdminInterface(t *testing.T) {
	var c gitoperationslib.Client = fakeForgejo(t, http.NotFound)
	if _, ok := c.(gitoperationslib.Admin); !ok || !c.Capabilities().Has(gitoperationslib.CapAdmin) {
		t.Error("Forgejo client should implement Admin and report CapAdmin")
	}
}
