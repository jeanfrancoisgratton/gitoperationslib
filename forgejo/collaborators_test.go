package forgejo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const collaboratorsBase = "/api/v1/repos/acme/widget/collaborators"

func TestListCollaborators(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case collaboratorsBase:
			w.Header().Set("X-Total-Count", "3")
			io.WriteString(w, `[{"login":"alice"},{"login":"bob"}]`)
		case collaboratorsBase + "/alice/permission":
			io.WriteString(w, `{"permission":"read","role_name":"read"}`)
		case collaboratorsBase + "/bob/permission":
			io.WriteString(w, `{"permission":"admin","role_name":"admin"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	page, err := c.Collaborators().List(context.Background(), "acme", "widget", gitoperationslib.ListOptions{PerPage: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.Collaborator{{Login: "alice", Permission: "read"}, {Login: "bob", Permission: "admin"}}
	if !slices.Equal(page.Items, want) || page.NextPage != 2 {
		t.Errorf("page = %+v", page)
	}
}

// Only a repository's admins may read the permissions of others, but anybody
// with access may list its collaborators.
func TestListCollaboratorsNotAdmin(t *testing.T) {
	refused := 0
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == collaboratorsBase {
			io.WriteString(w, `[{"login":"alice"},{"login":"bob"}]`)
			return
		}
		refused++
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"Only admins can query all permissions"}`)
	})
	page, err := c.Collaborators().List(context.Background(), "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.Collaborator{{Login: "alice"}, {Login: "bob"}}
	if !slices.Equal(page.Items, want) || refused != 1 {
		t.Errorf("items = %+v after %d refusals", page.Items, refused)
	}
}

func TestListCollaboratorsPermissionError(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == collaboratorsBase {
			io.WriteString(w, `[{"login":"alice"}]`)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	})
	if _, err := c.Collaborators().List(context.Background(), "acme", "widget", gitoperationslib.ListOptions{}); gitoperationslib.StatusCode(err) != http.StatusTeapot {
		t.Errorf("err = %v", err)
	}
}

func TestGetCollaborator(t *testing.T) {
	permissionStatus := http.StatusOK
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case collaboratorsBase + "/alice":
			w.WriteHeader(http.StatusNoContent)
		case collaboratorsBase + "/alice/permission":
			w.WriteHeader(permissionStatus)
			io.WriteString(w, `{"permission":"write"}`)
		case collaboratorsBase + "/owner":
			w.WriteHeader(http.StatusNotFound) // whatever its permission endpoint says
		case collaboratorsBase + "/nobody":
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"user does not exist [uid: 0, name: nobody]"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	got, err := c.Collaborators().Get(ctx, "acme", "widget", "alice")
	if err != nil || got.Login != "alice" || got.Permission != gitoperationslib.CollaboratorWrite {
		t.Errorf("Get = %+v, %v", got, err)
	}
	permissionStatus = http.StatusForbidden
	if got, err = c.Collaborators().Get(ctx, "acme", "widget", "alice"); err != nil || got.Permission != "" {
		t.Errorf("Get without the right to read permissions = %+v, %v", got, err)
	}
	permissionStatus = http.StatusInternalServerError
	if _, err = c.Collaborators().Get(ctx, "acme", "widget", "alice"); gitoperationslib.StatusCode(err) != http.StatusInternalServerError {
		t.Errorf("Get with a server error = %v", err)
	}
	for _, user := range []string{"owner", "nobody"} {
		if _, err := c.Collaborators().Get(ctx, "acme", "widget", user); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Get(%s) = %v, want 404", user, err)
		}
	}
	if _, err := c.Collaborators().Get(ctx, "acme", "widget", ""); err == nil {
		t.Error("Get without a user succeeded")
	}
}

func TestAddCollaborator(t *testing.T) {
	var bodies []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != collaboratorsBase+"/bob" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"user does not exist [uid: 0, name: nobody]"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, strings.TrimSpace(string(b)))
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	invited, err := c.Collaborators().Add(ctx, "acme", "widget", "bob", gitoperationslib.CollaboratorAdmin)
	if err != nil || invited {
		t.Errorf("Add = %v, %v", invited, err)
	}
	if want := []string{`{"permission":"admin"}`}; !slices.Equal(bodies, want) {
		t.Errorf("bodies = %q", bodies)
	}
	if _, err := c.Collaborators().Add(ctx, "acme", "widget", "nobody", gitoperationslib.CollaboratorRead); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Add(nobody) = %v, want 404", err)
	}
	// Forgejo would store write for these.
	if _, err := c.Collaborators().Add(ctx, "acme", "widget", "bob", gitoperationslib.CollaboratorTriage); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Add(triage) = %v", err)
	}
	for _, p := range []gitoperationslib.CollaboratorPermission{"", "owner"} {
		if _, err := c.Collaborators().Add(ctx, "acme", "widget", "bob", p); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("Add(%q) = %v", p, err)
		}
	}
	if _, err := c.Collaborators().Add(ctx, "acme", "widget", "", gitoperationslib.CollaboratorRead); err == nil {
		t.Error("Add without a user succeeded")
	}
	if len(bodies) != 1 {
		t.Errorf("%d requests sent", len(bodies))
	}
}

func TestRemoveCollaborator(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == collaboratorsBase+"/bob" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"message":"user does not exist [uid: 0, name: nobody]"}`)
	})
	ctx := context.Background()
	if err := c.Collaborators().Remove(ctx, "acme", "widget", "bob"); err != nil {
		t.Error(err)
	}
	if err := c.Collaborators().Remove(ctx, "acme", "widget", "nobody"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Remove(nobody) = %v, want 404", err)
	}
	if err := c.Collaborators().Remove(ctx, "acme", "widget", ""); err == nil {
		t.Error("Remove without a user succeeded")
	}
}
