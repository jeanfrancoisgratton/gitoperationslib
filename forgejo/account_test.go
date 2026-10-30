package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const gpgResponse = `{"id": 3, "key_id": "F4578977FE08E756", "public_key": "xjME", "verified": true,
	"emails": [{"email": "me@example.com", "verified": true}], "can_sign": true,
	"created_at": "2026-10-30T11:33:41Z", "expires_at": "0001-01-01T00:00:00Z"}`

func TestAccountRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/user/emails", "POST /api/v1/user/emails":
			w.WriteHeader(map[string]int{"GET": 200, "POST": 201}[r.Method])
			io.WriteString(w, `[{"email": "new@example.com", "primary": false, "verified": false}]`)
		case "DELETE /api/v1/user/emails", "DELETE /api/v1/user/keys/5", "DELETE /api/v1/user/gpg_keys/3":
			w.WriteHeader(http.StatusNoContent)
		case "POST /api/v1/user/keys", "GET /api/v1/user/keys/5":
			io.WriteString(w, `{"id": 5, "title": "laptop", "key": "ssh-ed25519 AAA", "read_only": true}`)
		case "GET /api/v1/user/keys":
			io.WriteString(w, `[{"id": 5, "title": "laptop", "key": "ssh-ed25519 AAA"}]`)
		case "POST /api/v1/user/gpg_keys", "POST /api/v1/user/gpg_key_verify", "GET /api/v1/user/gpg_keys/3":
			io.WriteString(w, gpgResponse)
		case "GET /api/v1/user/gpg_keys":
			io.WriteString(w, "["+gpgResponse+"]")
		case "GET /api/v1/user/gpg_key_token":
			if r.Header.Get("Accept") != "text/plain" {
				t.Errorf("token request Accept = %q", r.Header.Get("Accept"))
			}
			io.WriteString(w, "1b2e73da")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	if list, err := c.Emails().List(ctx); err != nil || len(list) != 1 || list[0].Address != "new@example.com" {
		t.Errorf("Emails.List = %+v, %v", list, err)
	}
	if added, err := c.Emails().Add(ctx, "new@example.com"); err != nil || len(added) != 1 || added[0].Verified {
		t.Errorf("Emails.Add = %+v, %v", added, err)
	}
	if err := c.Emails().Delete(ctx, "new@example.com"); err != nil {
		t.Error(err)
	}
	key, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "laptop", Key: "ssh-ed25519 AAA", ReadOnly: true})
	if err != nil || key.ID != 5 || !key.ReadOnly {
		t.Errorf("SSHKeys.Add = %+v, %v", key, err)
	}
	if page, err := c.SSHKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("SSHKeys.List = %+v, %v", page, err)
	}
	if got, err := c.SSHKeys().Get(ctx, 5); err != nil || got.Title != "laptop" {
		t.Errorf("SSHKeys.Get = %+v, %v", got, err)
	}
	if err := c.SSHKeys().Delete(ctx, 5); err != nil {
		t.Error(err)
	}
	gpg, err := c.GPGKeys().Add(ctx, "-----BEGIN PGP PUBLIC KEY BLOCK-----", "-----BEGIN PGP SIGNATURE-----")
	if err != nil || gpg.ID != 3 || gpg.KeyID != "F4578977FE08E756" || !gpg.Verified || !gpg.ExpiresAt.IsZero() {
		t.Errorf("GPGKeys.Add = %+v, %v", gpg, err)
	}
	if token, err := c.GPGKeys().VerificationToken(ctx); err != nil || token != "1b2e73da" {
		t.Errorf("VerificationToken = %q, %v", token, err)
	}
	if v, err := c.GPGKeys().Verify(ctx, "F4578977FE08E756", "sig"); err != nil || !v.Verified {
		t.Errorf("Verify = %+v, %v", v, err)
	}
	if page, err := c.GPGKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("GPGKeys.List = %+v, %v", page, err)
	}
	if got, err := c.GPGKeys().Get(ctx, 3); err != nil || got.KeyID != "F4578977FE08E756" {
		t.Errorf("GPGKeys.Get = %+v, %v", got, err)
	}
	if err := c.GPGKeys().Delete(ctx, 3); err != nil {
		t.Error(err)
	}

	wantBodies := map[int]map[string]any{
		1: {"emails": []any{"new@example.com"}},
		2: {"emails": []any{"new@example.com"}},
		3: {"title": "laptop", "key": "ssh-ed25519 AAA", "read_only": true},
		7: {"armored_public_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----", "armored_signature": "-----BEGIN PGP SIGNATURE-----"},
		9: {"key_id": "F4578977FE08E756", "armored_signature": "sig"},
	}
	for i, want := range wantBodies {
		if !reflect.DeepEqual(bodies[i], want) {
			t.Errorf("%s: body %v, want %v", requests[i], bodies[i], want)
		}
	}
}

func TestAccountValidationAndRefusals(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"None of the emails attached to the GPG key could be found. It may still be added if you provide a valid signature for the token: 1b2e"}`)
	})
	ctx := context.Background()
	_, err := c.GPGKeys().Add(ctx, "key", "")
	if !errors.Is(err, gitoperationslib.ErrGPGSignature) || !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown addresses: err = %v, want ErrGPGSignature (404 kept)", err)
	}
	for name, err := range map[string]error{
		"emails.add":    func() error { _, err := c.Emails().Add(ctx); return err }(),
		"emails.delete": c.Emails().Delete(ctx),
		"sshkeys.add": func() error {
			_, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "x"})
			return err
		}(),
		"gpgkeys.add":    func() error { _, err := c.GPGKeys().Add(ctx, "", ""); return err }(),
		"gpgkeys.verify": func() error { _, err := c.GPGKeys().Verify(ctx, "", "sig"); return err }(),
	} {
		if err == nil || gitoperationslib.StatusCode(err) != 0 {
			t.Errorf("%s: err = %v, want a validation error before any request", name, err)
		}
	}
}

func TestGPGVerifyLeadingZero(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"message":"public gpg key does not exist [id: 0]"}`)
	})
	ctx := context.Background()
	_, err := c.GPGKeys().Verify(ctx, "0FD9BAB20C8D9AB9", "sig")
	if !errors.Is(err, gitoperationslib.ErrNotSupported) || gitoperationslib.StatusCode(err) != 500 {
		t.Errorf("leading zero: err = %v, want ErrNotSupported with status 500", err)
	}
	if _, err := c.GPGKeys().Verify(ctx, "FD9BAB20C8D9AB90", "sig"); errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("no leading zero: err = %v, want a plain 500", err)
	}
}

func TestProfileForgejo(t *testing.T) {
	var requests []string
	var patched map[string]any
	var avatar map[string]string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "PATCH /api/v1/user/settings":
			json.NewDecoder(r.Body).Decode(&patched)
			io.WriteString(w, `{}`)
		case "POST /api/v1/user/avatar":
			json.NewDecoder(r.Body).Decode(&avatar)
			w.WriteHeader(http.StatusNoContent)
		case "DELETE /api/v1/user/avatar":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	u, err := c.Profile().Edit(ctx, gitoperationslib.EditProfileOptions{
		FullName: gitoperationslib.Ptr("Jean"), Website: gitoperationslib.Ptr(""), Pronouns: gitoperationslib.Ptr("they/them"),
		Settings: &gitoperationslib.EditSettings{Theme: gitoperationslib.Ptr("forgejo-dark"), HideEmail: gitoperationslib.Ptr(false)},
	})
	if err != nil || u.Login != "me" {
		t.Fatalf("Edit = %+v, %v", u, err)
	}
	want := map[string]any{"full_name": "Jean", "website": "", "pronouns": "they/them", "theme": "forgejo-dark", "hide_email": false}
	if !reflect.DeepEqual(patched, want) {
		t.Errorf("PATCH body %v, want %v", patched, want)
	}

	if err := c.Profile().SetAvatar(ctx, strings.NewReader("PNGDATA")); err != nil || avatar["image"] != "UE5HREFUQQ==" {
		t.Errorf("SetAvatar: %v, body %v", err, avatar)
	}
	if err := c.Profile().DeleteAvatar(ctx); err != nil {
		t.Error(err)
	}

	n := len(requests)
	if _, err := c.Profile().Edit(ctx, gitoperationslib.EditProfileOptions{Company: gitoperationslib.Ptr("Acme")}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("GitHub-only field: err = %v, want ErrNotSupported", err)
	}
	if err := c.Profile().SetAvatar(ctx, strings.NewReader("")); err == nil {
		t.Error("empty avatar accepted")
	}
	if err := c.Profile().SetAvatar(ctx, io.LimitReader(zeros{}, gitoperationslib.MaxAvatarSize+1)); err == nil {
		t.Error("oversized avatar accepted")
	}
	if len(requests) != n {
		t.Errorf("refused calls sent %v", requests[n:])
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestTokensForgejo(t *testing.T) {
	var requests []string
	var created map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/users/me/tokens":
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id": 3, "name": "ci", "sha1": "11af98fd0c193dda1961f3fb4926d6375f7f7a19", "token_last_eight": "5f7f7a19",
				"scopes": ["read:repository"], "repositories": [{"id": 54, "name": "r1", "owner": "me", "full_name": "me/r1"}]}`)
		case "GET /api/v1/users/me/tokens":
			io.WriteString(w, `[{"id": 6, "name": "123", "sha1": "", "token_last_eight": "aaaaaaaa", "scopes": ["all"], "repositories": null}]`)
		case "DELETE /api/v1/users/me/tokens/6":
			w.WriteHeader(http.StatusNoContent)
		case "DELETE /api/v1/users/me/tokens/9":
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"auth method not allowed"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	tok, err := c.Tokens().Create(ctx, gitoperationslib.CreateTokenOptions{Name: "ci", Scopes: []string{"read:repository"}, Repositories: []string{"me/r1"}})
	if err != nil || tok.Secret != "11af98fd0c193dda1961f3fb4926d6375f7f7a19" || tok.LastEight != "5f7f7a19" ||
		!reflect.DeepEqual(tok.Repositories, []string{"me/r1"}) {
		t.Errorf("Create = %+v, %v", tok, err)
	}
	want := map[string]any{"name": "ci", "scopes": []any{"read:repository"}, "repositories": []any{map[string]any{"owner": "me", "name": "r1"}}}
	if !reflect.DeepEqual(created, want) {
		t.Errorf("POST body %v, want %v", created, want)
	}
	if page, err := c.Tokens().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 || page.Items[0].Repositories != nil {
		t.Errorf("List = %+v, %v", page, err)
	}
	// "123" is found by listing and deleted by ID 6, not taken for ID 123.
	if err := c.Tokens().DeleteByName(ctx, "123"); err != nil {
		t.Error(err)
	}
	if err := c.Tokens().DeleteByName(ctx, "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("DeleteByName(unknown): err = %v, want 404", err)
	}
	if err := c.Tokens().Delete(ctx, 9); !errors.Is(err, gitoperationslib.ErrPasswordRequired) {
		t.Errorf("Delete with a token: err = %v, want ErrPasswordRequired", err)
	}

	n := len(requests)
	for _, bad := range []gitoperationslib.CreateTokenOptions{
		{Scopes: []string{"all"}},
		{Name: "x", Scopes: []string{"all"}, Repositories: []string{"no-slash"}},
		{Name: "x", Scopes: []string{"all"}, Repositories: []string{"a/b/c"}},
	} {
		if _, err := c.Tokens().Create(ctx, bad); err == nil {
			t.Errorf("Create(%+v) accepted", bad)
		}
	}
	if len(requests) != n {
		t.Errorf("invalid options sent %v", requests[n:])
	}
	if got := strings.Join(requests[:n], ", "); !strings.Contains(got, "DELETE /api/v1/users/me/tokens/6") {
		t.Errorf("requests %s", got)
	}
}
