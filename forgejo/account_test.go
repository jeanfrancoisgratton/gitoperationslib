package forgejo

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
