package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const collaboratorsBase = "/api/v3/repos/acme/widget/collaborators"

func TestListCollaboratorsGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != collaboratorsBase || r.URL.Query().Get("affiliation") != "direct" {
			t.Errorf("unexpected %s", r.URL)
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
		io.WriteString(w, `[{"login":"alice","role_name":"maintain"},{"login":"bob","role_name":"auditor"}]`)
	})
	page, err := c.ListCollaborators(context.Background(), "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.Collaborator{
		{Login: "alice", Permission: gitoperationslib.CollaboratorMaintain},
		{Login: "bob", Permission: "auditor"}, // a custom role
	}
	if !slices.Equal(page.Items, want) || page.NextPage != 2 {
		t.Errorf("page = %+v", page)
	}
}

func TestGetCollaboratorGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case collaboratorsBase + "/alice":
			w.WriteHeader(http.StatusNoContent)
		case collaboratorsBase + "/alice/permission":
			// permission only knows admin, write and read.
			io.WriteString(w, `{"permission":"write","role_name":"maintain"}`)
		case collaboratorsBase + "/stranger":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	got, err := c.GetCollaborator(ctx, "acme", "widget", "alice")
	if err != nil || got.Login != "alice" || got.Permission != gitoperationslib.CollaboratorMaintain {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := c.GetCollaborator(ctx, "acme", "widget", "stranger"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(stranger) = %v, want 404", err)
	}
	if _, err := c.GetCollaborator(ctx, "acme", "widget", ""); err == nil {
		t.Error("Get without a user succeeded")
	}
}

func TestAddCollaboratorGitHub(t *testing.T) {
	var sent []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		var body struct {
			Permission string `json:"permission"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body.Permission)
		switch r.URL.Path {
		case collaboratorsBase + "/invited":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":7,"invitee":{"login":"invited"}}`)
		case collaboratorsBase + "/member":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	})
	ctx := context.Background()
	if invited, err := c.AddCollaborator(ctx, "acme", "widget", "invited", gitoperationslib.CollaboratorRead); !invited || err != nil {
		t.Errorf("Add(invited) = %v, %v", invited, err)
	}
	if invited, err := c.AddCollaborator(ctx, "acme", "widget", "member", gitoperationslib.CollaboratorWrite); invited || err != nil {
		t.Errorf("Add(member) = %v, %v", invited, err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "gone", gitoperationslib.CollaboratorTriage); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Add(gone) = %v", err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "member", gitoperationslib.CollaboratorMaintain); err != nil {
		t.Error(err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "member", gitoperationslib.CollaboratorAdmin); err != nil {
		t.Error(err)
	}
	if want := []string{"pull", "push", "triage", "maintain", "admin"}; !slices.Equal(sent, want) {
		t.Errorf("permissions sent = %q, want %q", sent, want)
	}
	for _, p := range []gitoperationslib.CollaboratorPermission{"", "owner"} {
		if _, err := c.AddCollaborator(ctx, "acme", "widget", "member", p); err == nil {
			t.Errorf("Add(%q) succeeded", p)
		}
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "", gitoperationslib.CollaboratorRead); err == nil {
		t.Error("Add without a user succeeded")
	}
	if len(sent) != 5 {
		t.Errorf("%d requests sent", len(sent))
	}
}

// Removing does not touch an invitation, which the user could still accept.
func TestRemoveCollaboratorGitHub(t *testing.T) {
	const invitations = "/api/v3/repos/acme/widget/invitations"
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/v3/repos/acme/widget/"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == invitations && r.URL.Query().Get("page") == "":
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
			io.WriteString(w, `[{"id":1,"invitee":{"login":"other"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == invitations:
			io.WriteString(w, `[{"id":2,"invitee":{"login":"Bob"}}]`)
		case r.URL.Path == collaboratorsBase+"/gone":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	ctx := context.Background()
	if err := c.RemoveCollaborator(ctx, "acme", "widget", "bob"); err != nil {
		t.Fatal(err)
	}
	want := []string{"DELETE collaborators/bob", "GET invitations", "GET invitations", "DELETE invitations/2"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %q, want %q", requests, want)
	}
	if err := c.RemoveCollaborator(ctx, "acme", "widget", "gone"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Remove(gone) = %v", err)
	}
	if err := c.RemoveCollaborator(ctx, "acme", "widget", ""); err == nil {
		t.Error("Remove without a user succeeded")
	}
}

func TestListCollaboratorInvitations(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/repos/acme/widget/invitations" {
			t.Errorf("unexpected %s", r.URL)
		}
		if r.URL.Query().Get("page") == "9" {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"Must have admin rights"}`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
		io.WriteString(w, `[{"id":7,"invitee":{"login":"bob"},"permissions":"write","expired":true,"html_url":"https://g/i/7","created_at":"2026-11-05T10:00:00Z"}]`)
	})
	ctx := context.Background()
	page, err := c.ListCollaboratorInvitations(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("page = %+v", page)
	}
	i := page.Items[0]
	if i.ID != 7 || i.Login != "bob" || i.Permission != gitoperationslib.CollaboratorWrite || !i.Expired || i.HTMLURL != "https://g/i/7" || i.CreatedAt.IsZero() {
		t.Errorf("invitation = %+v", i)
	}
	if _, err := c.ListCollaboratorInvitations(ctx, "acme", "widget", gitoperationslib.ListOptions{Page: 9}); gitoperationslib.StatusCode(err) != http.StatusForbidden {
		t.Errorf("err = %v", err)
	}
}
