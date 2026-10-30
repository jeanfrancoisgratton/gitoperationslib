package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestAccountBitbucket(t *testing.T) {
	var requests []string
	var posted []map[string]string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			posted = append(posted, body)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /2.0/user/emails":
			io.WriteString(w, `{"values": [{"email": "a@example.com", "is_primary": true, "is_confirmed": true}]}`)
		case "GET /2.0/users/{me}/ssh-keys":
			io.WriteString(w, `{"values": [{"uuid": "{f54f087e-8f59-460d-ac9f-5bc30a051d32}", "key": "ssh-ed25519 AAA", "label": "laptop"}]}`)
		case "POST /2.0/users/{me}/ssh-keys":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"uuid": "{f54f087e-8f59-460d-ac9f-5bc30a051d32}", "key": "ssh-ed25519 AAA", "label": "laptop"}`)
		case "GET /2.0/users/{me}/gpg-keys":
			io.WriteString(w, `{"values": [{"key_id": "c74d6c4a818e3cf3", "fingerprint": "43e5087282eebe7546178fb7c74d6c4a818e3cf3"}]}`)
		case "POST /2.0/users/{me}/gpg-keys":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"key_id": "c74d6c4a818e3cf3", "fingerprint": "43e5087282eebe7546178fb7c74d6c4a818e3cf3"}`)
		case "DELETE /2.0/users/{me}/ssh-keys/{f54f087e-8f59-460d-ac9f-5bc30a051d32}",
			"DELETE /2.0/users/{me}/gpg-keys/43e5087282eebe7546178fb7c74d6c4a818e3cf3":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	const sshID, gpgID = 0xf54f087e8f59460, 0x43e5087282eebe7

	if list, err := c.Emails().List(ctx); err != nil || len(list) != 1 || !list[0].Primary {
		t.Errorf("Emails.List = %+v, %v", list, err)
	}
	if _, err := c.Emails().Add(ctx, "b@example.com"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Emails.Add: err = %v, want ErrNotSupported", err)
	}
	if err := c.Emails().Delete(ctx, "a@example.com"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Emails.Delete: err = %v, want ErrNotSupported", err)
	}

	key, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "laptop", Key: "ssh-ed25519 AAA"})
	if err != nil || key.ID != sshID || key.Title != "laptop" {
		t.Errorf("SSHKeys.Add = %+v, %v", key, err)
	}
	if posted[0]["label"] != "laptop" || posted[0]["key"] != "ssh-ed25519 AAA" {
		t.Errorf("posted %v", posted[0])
	}
	if got, err := c.SSHKeys().Get(ctx, sshID); err != nil || got.Title != "laptop" {
		t.Errorf("SSHKeys.Get = %+v, %v", got, err)
	}
	if _, err := c.SSHKeys().Get(ctx, 42); !gitoperationslib.IsNotFound(err) {
		t.Errorf("SSHKeys.Get(unknown): err = %v, want 404", err)
	}
	if page, err := c.SSHKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("SSHKeys.List = %+v, %v", page, err)
	}
	if err := c.SSHKeys().Delete(ctx, sshID); err != nil {
		t.Error(err)
	}

	gpg, err := c.GPGKeys().Add(ctx, "-----BEGIN PGP PUBLIC KEY BLOCK-----", "")
	if err != nil || gpg.ID != gpgID || gpg.KeyID != "C74D6C4A818E3CF3" {
		t.Errorf("GPGKeys.Add = %+v, %v", gpg, err)
	}
	if got, err := c.GPGKeys().Get(ctx, gpgID); err != nil || got.ID != gpgID {
		t.Errorf("GPGKeys.Get = %+v, %v", got, err)
	}
	if page, err := c.GPGKeys().List(ctx, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("GPGKeys.List = %+v, %v", page, err)
	}
	if err := c.GPGKeys().Delete(ctx, gpgID); err != nil {
		t.Error(err)
	}
	if _, err := c.GPGKeys().Add(ctx, "key", "sig"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("signed GPG key: err = %v, want ErrNotSupported", err)
	}
	if _, err := c.GPGKeys().VerificationToken(ctx); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("VerificationToken: err = %v, want ErrNotSupported", err)
	}
	if _, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: "t", Key: "k", ReadOnly: true}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("read-only key: err = %v, want ErrNotSupported", err)
	}
}

func TestProfileAndTokensNotSupportedBitbucket(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s %s", r.Method, r.URL.Path) })
	ctx := context.Background()
	_, errEdit := c.Profile().Edit(ctx, gitoperationslib.EditProfileOptions{})
	_, errList := c.Tokens().List(ctx, gitoperationslib.ListOptions{})
	_, errCreate := c.Tokens().Create(ctx, gitoperationslib.CreateTokenOptions{Name: "x"})
	for _, err := range []error{errEdit, c.Profile().SetAvatar(ctx, nil), c.Profile().DeleteAvatar(ctx),
		errList, errCreate, c.Tokens().Delete(ctx, 1), c.Tokens().DeleteByName(ctx, "x")} {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("err = %v, want ErrNotSupported", err)
		}
	}
}
