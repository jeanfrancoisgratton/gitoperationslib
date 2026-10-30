package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestAccountGitHub(t *testing.T) {
	var requests []string
	var bodies []any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/user/emails", "POST /api/v3/user/emails":
			io.WriteString(w, `[{"email": "a@example.com", "primary": true, "verified": true, "visibility": "private"}]`)
		case "DELETE /api/v3/user/emails", "DELETE /api/v3/user/keys/5", "DELETE /api/v3/user/gpg_keys/3":
			w.WriteHeader(http.StatusNoContent)
		case "POST /api/v3/user/keys", "GET /api/v3/user/keys/5":
			io.WriteString(w, `{"id": 5, "title": "laptop", "key": "ssh-ed25519 AAA"}`)
		case "GET /api/v3/user/keys":
			io.WriteString(w, `[{"id": 5, "title": "laptop", "key": "ssh-ed25519 AAA"}]`)
		case "POST /api/v3/user/gpg_keys", "GET /api/v3/user/gpg_keys/3":
			io.WriteString(w, `{"id": 3, "key_id": "3262EFF25BA0D270", "public_key": "xsBN", "can_sign": true}`)
		case "GET /api/v3/user/gpg_keys":
			io.WriteString(w, `[{"id": 3, "key_id": "3262EFF25BA0D270"}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	if list, err := c.Emails().List(ctx); err != nil || len(list) != 1 || list[0].Visibility != "private" {
		t.Errorf("Emails.List = %+v, %v", list, err)
	}
	if _, err := c.Emails().Add(ctx, "b@example.com"); err != nil {
		t.Error(err)
	}
	if err := c.Emails().Delete(ctx, "b@example.com"); err != nil {
		t.Error(err)
	}
	if key, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "laptop", Key: "ssh-ed25519 AAA"}); err != nil || key.ID != 5 {
		t.Errorf("SSHKeys.Add = %+v, %v", key, err)
	}
	if page, err := c.SSHKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("SSHKeys.List = %+v, %v", page, err)
	}
	if _, err := c.SSHKeys().Get(ctx, 5); err != nil {
		t.Error(err)
	}
	if err := c.SSHKeys().Delete(ctx, 5); err != nil {
		t.Error(err)
	}
	if key, err := c.GPGKeys().Add(ctx, "-----BEGIN PGP PUBLIC KEY BLOCK-----", ""); err != nil || key.KeyID != "3262EFF25BA0D270" {
		t.Errorf("GPGKeys.Add = %+v, %v", key, err)
	}
	if page, err := c.GPGKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("GPGKeys.List = %+v, %v", page, err)
	}
	if _, err := c.GPGKeys().Get(ctx, 3); err != nil {
		t.Error(err)
	}
	if err := c.GPGKeys().Delete(ctx, 3); err != nil {
		t.Error(err)
	}

	for i, want := range map[int]any{
		1: []any{"b@example.com"},
		2: []any{"b@example.com"},
		3: map[string]any{"title": "laptop", "key": "ssh-ed25519 AAA"},
		7: map[string]any{"armored_public_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----"},
	} {
		if !reflect.DeepEqual(bodies[i], want) {
			t.Errorf("%s: body %v, want %v", requests[i], bodies[i], want)
		}
	}

	// Forgejo-only features are refused before any request.
	n := len(requests)
	for name, err := range map[string]error{
		"read-only key": func() error {
			_, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "t", Key: "k", ReadOnly: true})
			return err
		}(),
		"signed GPG key": func() error { _, err := c.GPGKeys().Add(ctx, "key", "sig"); return err }(),
		"token":          func() error { _, err := c.GPGKeys().VerificationToken(ctx); return err }(),
		"verify":         func() error { _, err := c.GPGKeys().Verify(ctx, "id", "sig"); return err }(),
	} {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if len(requests) != n {
		t.Errorf("refused calls sent %v", requests[n:])
	}
}

func TestProfileGitHub(t *testing.T) {
	var body map[string]any
	calls := 0
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v3/user" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"login": "me", "name": "Jean", "bio": "b", "blog": "", "company": "Acme"}`)
	})
	ctx := context.Background()
	u, err := c.Profile().Edit(ctx, gitoperationslib.EditProfileOptions{
		FullName: gitoperationslib.Ptr("Jean"), Description: gitoperationslib.Ptr("b"), Website: gitoperationslib.Ptr(""),
		Company: gitoperationslib.Ptr("Acme"), Hireable: gitoperationslib.Ptr(true),
	})
	if err != nil || u.FullName != "Jean" || u.Description != "b" || u.Company != "Acme" {
		t.Fatalf("Edit = %+v, %v", u, err)
	}
	// An empty string is sent, and clears the field.
	want := map[string]any{"name": "Jean", "bio": "b", "blog": "", "company": "Acme", "hireable": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("PATCH body %v, want %v", body, want)
	}
	for name, err := range map[string]error{
		"pronouns": func() error {
			_, err := c.Profile().Edit(ctx, gitoperationslib.EditProfileOptions{Pronouns: gitoperationslib.Ptr("x")})
			return err
		}(),
		"avatar": c.Profile().SetAvatar(ctx, nil),
	} {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if calls != 1 {
		t.Errorf("%d requests, want 1", calls)
	}
}
