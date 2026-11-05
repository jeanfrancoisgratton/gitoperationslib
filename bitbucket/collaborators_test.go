package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const collaboratorsBase = "/2.0/repositories/acme/widget/permissions-config/users"

func TestListCollaborators(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != collaboratorsBase || r.URL.Query().Get("page") != "2" {
			t.Errorf("unexpected %s", r.URL)
		}
		io.WriteString(w, `{"values":[{"permission":"admin","user":{"uuid":"{alice}","nickname":"alice","account_id":"70121:a"}}],"page":2,"next":"https://x/?page=3"}`)
	})
	page, err := c.ListCollaborators(context.Background(), "acme", "widget", gitoperationslib.ListOptions{Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := gitoperationslib.Collaborator{Login: "alice", AccountID: "70121:a", Permission: gitoperationslib.CollaboratorAdmin}
	if len(page.Items) != 1 || page.Items[0] != want || page.NextPage != 3 {
		t.Errorf("page = %+v", page)
	}
}

func TestGetCollaborator(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case collaboratorsBase + "/%7Balice%7D", collaboratorsBase + "/70121:a":
			io.WriteString(w, `{"permission":"write","user":{"nickname":"alice","account_id":"70121:a"}}`)
		case collaboratorsBase + "/%7Bbob%7D":
			// A user without access is no error.
			io.WriteString(w, `{"permission":"none","user":{"nickname":"bob"}}`)
		case collaboratorsBase + "/70121:gone":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"No user"}}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	for _, user := range []string{"alice", "{alice}", "70121:a"} {
		got, err := c.GetCollaborator(ctx, "acme", "widget", user)
		if err != nil || got.Login != "alice" || got.Permission != gitoperationslib.CollaboratorWrite {
			t.Errorf("Get(%s) = %+v, %v", user, got, err)
		}
	}
	for _, user := range []string{"bob", "70121:gone", "not-a-member"} {
		if _, err := c.GetCollaborator(ctx, "acme", "widget", user); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Get(%s) = %v, want 404", user, err)
		}
	}
	if _, err := c.GetCollaborator(ctx, "acme", "widget", ""); err == nil {
		t.Error("Get without a user succeeded")
	}
}

func TestAddCollaborator(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		requests = append(requests, r.Method+" "+r.URL.EscapedPath()+" "+strings.TrimSpace(string(b)))
		io.WriteString(w, `{}`)
	})
	ctx := context.Background()
	// An account ID can be somebody outside the workspace.
	for _, user := range []string{"bob", "70121:outsider"} {
		if invited, err := c.AddCollaborator(ctx, "acme", "widget", user, gitoperationslib.CollaboratorRead); invited || err != nil {
			t.Errorf("Add(%s) = %v, %v", user, invited, err)
		}
	}
	want := []string{
		"PUT " + collaboratorsBase + `/%7Bbob%7D {"permission":"read"}`,
		"PUT " + collaboratorsBase + `/70121:outsider {"permission":"read"}`,
	}
	if len(requests) != 2 || requests[0] != want[0] || requests[1] != want[1] {
		t.Errorf("requests = %q", requests)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "bob", gitoperationslib.CollaboratorMaintain); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Add(maintain) = %v", err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "bob", ""); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Add without a permission = %v", err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "stranger", gitoperationslib.CollaboratorRead); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Add(stranger) = %v", err)
	}
	if _, err := c.AddCollaborator(ctx, "acme", "widget", "", gitoperationslib.CollaboratorRead); err == nil {
		t.Error("Add without a user succeeded")
	}
	if len(requests) != 2 {
		t.Errorf("%d requests sent", len(requests))
	}
}

func TestRemoveCollaborator(t *testing.T) {
	var got string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	if err := c.RemoveCollaborator(ctx, "acme", "widget", "{bob}"); err != nil {
		t.Fatal(err)
	}
	if want := "DELETE " + collaboratorsBase + "/%7Bbob%7D"; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
	if err := c.RemoveCollaborator(ctx, "acme", "widget", "stranger"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Remove(stranger) = %v", err)
	}
	if err := c.RemoveCollaborator(ctx, "acme", "widget", ""); err == nil {
		t.Error("Remove without a user succeeded")
	}
}
