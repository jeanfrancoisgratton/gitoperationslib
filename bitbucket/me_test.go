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

func TestMeConversion(t *testing.T) {
	var u meJSON
	err := json.Unmarshal([]byte(`{"uuid": "{me}", "account_id": "70121:me", "nickname": "alice", "display_name": "Alice",
		"location": "Montréal", "account_status": "active", "has_2fa_enabled": true,
		"links": {"html": {"href": "https://bitbucket.org/alice/"}, "avatar": {"href": "https://avatar/a"}},
		"created_on": "2026-10-17T10:00:00Z"}`), &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.login() != "alice" || u.Location != "Montréal" || u.Has2FAEnabled == nil || !*u.Has2FAEnabled ||
		u.Links.HTML.Href != "https://bitbucket.org/alice/" || u.Links.Avatar.Href != "https://avatar/a" || u.CreatedOn.IsZero() {
		t.Errorf("meJSON = %+v", u)
	}
}

func TestMe(t *testing.T) {
	var keysPath string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2.0/user/emails":
			io.WriteString(w, `{"values": [{"email": "b@example.com", "is_primary": false},
				{"email": "a@example.com", "is_primary": true, "is_confirmed": true}]}`)
		case "/2.0/repositories/acme":
			io.WriteString(w, `{"values": [{"slug": "widget", "full_name": "acme/widget", "workspace": {"slug": "acme"}}]}`)
		case "/2.0/users/{me}/ssh-keys":
			keysPath = r.URL.RawPath
			io.WriteString(w, `{"values": [{"key": "ssh-ed25519 AAA", "comment": "alice@laptop", "label": ""}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	me, err := c.Directory().Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.Login != "alice" || me.UUID != "{me}" || me.AccountID != "70121:me" || !me.Active {
		t.Errorf("CurrentUser = %+v", me)
	}
	if me.Email != "a@example.com" || len(me.Emails) != 2 || !me.Emails[1].Verified {
		t.Errorf("Email = %q, Emails = %+v", me.Email, me.Emails)
	}
	if len(me.Orgs) != 1 || me.Orgs[0].Name != "acme" || len(me.Repos) != 1 || me.Repos[0].FullName != "acme/widget" {
		t.Errorf("Orgs = %+v, Repos = %+v", me.Orgs, me.Repos)
	}
	if len(me.SSHKeys) != 1 || me.SSHKeys[0].Title != "alice@laptop" {
		t.Errorf("SSHKeys = %+v (path %s)", me.SSHKeys, keysPath)
	}
	if !errors.Is(me.Incomplete, gitoperationslib.ErrNotSupported) || len(me.Teams) != 0 || len(me.GPGKeys) != 0 {
		t.Errorf("Incomplete = %v, want teams and GPG keys not supported", me.Incomplete)
	}
}
