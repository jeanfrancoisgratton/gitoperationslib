package forgejo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestMeConversions(t *testing.T) {
	var u meJSON
	if err := json.Unmarshal([]byte(`{"login": "alice", "email": "a@example.com", "login_name": "alice@ldap", "source_id": 2}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.toUser().Email != "a@example.com" || u.LoginName != "alice@ldap" || u.SourceID != 2 {
		t.Errorf("meJSON = %+v", u)
	}

	var k gpgKeyJSON
	err := json.Unmarshal([]byte(`{"id": 1, "key_id": "ABCD", "public_key": "pk", "verified": true, "can_sign": true,
		"emails": [{"email": "a@example.com", "verified": true}],
		"subkeys": [{"id": 2, "key_id": "EF01", "can_encrypt_comms": true, "expires_at": "2030-01-01T00:00:00Z"}],
		"created_at": "2026-10-17T10:00:00Z", "expires_at": "1970-01-01T00:00:00Z"}`), &k)
	if err != nil {
		t.Fatal(err)
	}
	key := k.toGPGKey()
	if key.KeyID != "ABCD" || !key.Verified || !key.CanSign || !key.ExpiresAt.IsZero() ||
		len(key.Emails) != 1 || key.Emails[0] != (gitoperationslib.GPGKeyEmail{Address: "a@example.com", Verified: true}) {
		t.Errorf("GPG key = %+v", key)
	}
	if len(key.Subkeys) != 1 || key.Subkeys[0].KeyID != "EF01" || !key.Subkeys[0].CanEncryptComms ||
		!key.Subkeys[0].ExpiresAt.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("subkeys = %+v", key.Subkeys)
	}
}

// TestMe checks every part of Me, and that a refused part (SSH keys without
// the scope) leaves the result incomplete instead of failing it.
func TestMe(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/user/settings":
			io.WriteString(w, `{"theme": "forgejo-dark", "diff_view_style": "split", "hide_email": true}`)
		case "/api/v1/user/emails":
			io.WriteString(w, `[{"email": "a@example.com", "primary": true, "verified": true}, {"email": "b@example.com"}]`)
		case "/api/v1/user/orgs":
			io.WriteString(w, "["+orgResponse+"]")
		case "/api/v1/user/teams":
			io.WriteString(w, "["+teamResponse+"]")
		case "/api/v1/user/repos":
			io.WriteString(w, "["+repoResponse+"]")
		case "/api/v1/user/keys":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"token does not have at least one of required scope(s): [read:user]"}`)
		case "/api/v1/user/gpg_keys":
			io.WriteString(w, `[{"id": 1, "key_id": "ABCD"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	me, err := c.Directory().Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.Login != "me" {
		t.Errorf("Login = %q", me.Login)
	}
	if me.Settings == nil || me.Settings.Theme != "forgejo-dark" || me.Settings.DiffViewStyle != "split" || !me.Settings.HideEmail {
		t.Errorf("Settings = %+v", me.Settings)
	}
	if len(me.Emails) != 2 || !me.Emails[0].Primary || !me.Emails[0].Verified || me.Emails[1].Address != "b@example.com" {
		t.Errorf("Emails = %+v", me.Emails)
	}
	if len(me.Orgs) != 1 || me.Orgs[0].Name != "acme" || len(me.Teams) != 1 || me.Teams[0].Org != "acme" || len(me.Repos) != 1 {
		t.Errorf("Orgs = %+v, Teams = %+v, Repos = %d", me.Orgs, me.Teams, len(me.Repos))
	}
	if len(me.SSHKeys) != 0 || len(me.GPGKeys) != 1 || me.GPGKeys[0].KeyID != "ABCD" {
		t.Errorf("SSHKeys = %+v, GPGKeys = %+v", me.SSHKeys, me.GPGKeys)
	}
	if !gitoperationslib.IsForbidden(me.Incomplete) {
		t.Errorf("Incomplete = %v, want the 403 on SSH keys", me.Incomplete)
	}
	want := []string{"/api/v1/user/settings", "/api/v1/user/emails", "/api/v1/user/orgs", "/api/v1/user/teams",
		"/api/v1/user/repos", "/api/v1/user/keys", "/api/v1/user/gpg_keys"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}
}
