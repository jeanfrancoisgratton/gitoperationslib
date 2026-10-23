package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCurrentUserConversion(t *testing.T) {
	var u gh.User
	err := json.Unmarshal([]byte(`{"login": "alice", "type": "User", "two_factor_authentication": true,
		"plan": {"name": "pro"}, "public_repos": 3, "total_private_repos": 5, "owned_private_repos": 4,
		"disk_usage": 1024, "hireable": true, "twitter_username": "al", "updated_at": "2026-10-20T10:00:00Z"}`), &u)
	if err != nil {
		t.Fatal(err)
	}
	me := toCurrentUser(&u)
	if me.Login != "alice" || me.TwoFactorAuth == nil || !*me.TwoFactorAuth || me.Type != "User" || me.Plan != "pro" ||
		me.PublicRepos != 3 || me.TotalPrivateRepos != 5 || me.OwnedPrivateRepos != 4 || me.DiskUsage != 1024 ||
		!me.Hireable || me.TwitterUsername != "al" || !me.UpdatedAt.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("CurrentUser = %+v", me)
	}
	if toCurrentUser(&gh.User{}).TwoFactorAuth != nil {
		t.Error("TwoFactorAuth set although GitHub did not report it")
	}
}

// TestMe checks every part of Me; a token without user:email gets a 404 on
// the e-mail list, which leaves the result incomplete.
func TestMe(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/user/emails":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		case "/api/v3/user/orgs":
			io.WriteString(w, "["+orgResponse+"]")
		case "/api/v3/user/teams":
			io.WriteString(w, "["+teamResponse+"]")
		case "/api/v3/user/repos":
			if r.URL.Query().Get("affiliation") != "" {
				t.Errorf("repositories listed with affiliation %q, want the default", r.URL.Query().Get("affiliation"))
			}
			io.WriteString(w, "["+repoResponse+"]")
		case "/api/v3/user/keys":
			io.WriteString(w, `[{"id": 1, "title": "laptop", "key": "ssh-ed25519 AAA", "read_only": false,
				"created_at": "2026-10-17T10:00:00Z", "last_used": "2026-10-22T10:00:00Z"}]`)
		case "/api/v3/user/gpg_keys":
			io.WriteString(w, `[{"id": 2, "key_id": "ABCD", "can_sign": true, "emails": [{"email": "a@example.com", "verified": true}],
				"subkeys": [{"id": 3, "key_id": "EF01"}]}]`)
		default:
			http.NotFound(w, r)
		}
	})
	me, err := c.Directory().Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.Login != "me" || len(me.Emails) != 0 {
		t.Errorf("Login = %q, Emails = %+v", me.Login, me.Emails)
	}
	if len(me.Orgs) != 1 || len(me.Teams) != 1 || me.Teams[0].Org != "acme" || len(me.Repos) != 1 {
		t.Errorf("Orgs = %+v, Teams = %+v, Repos = %d", me.Orgs, me.Teams, len(me.Repos))
	}
	if len(me.SSHKeys) != 1 || me.SSHKeys[0].Title != "laptop" || me.SSHKeys[0].LastUsed.IsZero() {
		t.Errorf("SSHKeys = %+v", me.SSHKeys)
	}
	if len(me.GPGKeys) != 1 || !me.GPGKeys[0].CanSign || len(me.GPGKeys[0].Emails) != 1 ||
		len(me.GPGKeys[0].Subkeys) != 1 || me.GPGKeys[0].Subkeys[0].KeyID != "EF01" {
		t.Errorf("GPGKeys = %+v", me.GPGKeys)
	}
	if !gitoperationslib.IsNotFound(me.Incomplete) {
		t.Errorf("Incomplete = %v, want the 404 on e-mail addresses", me.Incomplete)
	}
}
