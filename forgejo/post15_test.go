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

func TestUserSecretsListByVersion(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		io.WriteString(w, `[{"name":"TOKEN","created_at":"2026-11-05T00:00:00Z"}]`)
	})
	ctx := context.Background()
	if _, err := c.Secrets().List(ctx, gitoperationslib.UserCI(), gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "17.0.0") {
		t.Errorf("before 17: %v", err)
	}
	c.version = Version{17, 0, 1}
	page, err := c.Secrets().List(ctx, gitoperationslib.UserCI(), gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "TOKEN" {
		t.Errorf("on 17: %+v, %v", page, err)
	}
	if !slices.Equal(requests, []string{"/api/v1/user/actions/secrets"}) {
		t.Errorf("requests = %v", requests)
	}
}

func TestListUsersBy(t *testing.T) {
	var query string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("is_2fa_enabled")
		io.WriteString(w, `[{"id":1,"login":"bob"}]`)
	})
	ctx := context.Background()
	with := gitoperationslib.UserFilter{TwoFactor: gitoperationslib.Ptr(false)}
	if _, err := c.ListUsersBy(ctx, with, gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "16.0.0") {
		t.Errorf("before 16: %v", err)
	}
	// No filter needs no version.
	if _, err := c.ListUsersBy(ctx, gitoperationslib.UserFilter{}, gitoperationslib.ListOptions{}); err != nil || query != "" {
		t.Errorf("no filter: %v, query %q", err, query)
	}
	c.version = Version{16, 0, 7}
	page, err := c.ListUsersBy(ctx, with, gitoperationslib.ListOptions{})
	if err != nil || query != "false" || len(page.Items) != 1 {
		t.Errorf("on 16: %+v, %v, query %q", page, err, query)
	}
}

func TestAddMemberInvitation(t *testing.T) {
	addStatus, memberStatus := http.StatusNoContent, http.StatusNotFound
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method)
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(addStatus)
			if addStatus == http.StatusConflict {
				io.WriteString(w, `{"message":"team invite already exists [team_id: 2, email: b@example.invalid, invited_user_id: 0]"}`)
			}
		default:
			w.WriteHeader(memberStatus)
			if memberStatus == http.StatusOK {
				io.WriteString(w, `{"login":"bob"}`)
			}
		}
	})
	ctx := context.Background()

	// Before 16 there is nothing to check.
	if err := c.Teams().AddMember(ctx, 2, "bob"); err != nil || !slices.Equal(requests, []string{"PUT"}) {
		t.Errorf("before 16: %v, requests %v", err, requests)
	}

	c.version = Version{17, 0, 1}
	if err := c.Teams().AddMember(ctx, 2, "bob"); !errors.Is(err, gitoperationslib.ErrInvitationPending) {
		t.Errorf("invited: %v", err)
	}
	addStatus = http.StatusConflict
	if err := c.Teams().AddMember(ctx, 2, "bob"); !errors.Is(err, gitoperationslib.ErrInvitationPending) {
		t.Errorf("invited again: %v", err)
	}
	addStatus, memberStatus = http.StatusNoContent, http.StatusOK
	if err := c.Teams().AddMember(ctx, 2, "bob"); err != nil {
		t.Errorf("added: %v", err)
	}
}

func TestPost15Fields(t *testing.T) {
	key := (&sshKeyJSON{}).toSSHKey()
	if !key.UpdatedAt.IsZero() || key.Verified {
		t.Errorf("zero key = %+v", key)
	}
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":5,"title":"k","key":"ssh-ed25519 AAA","updated_at":"2026-11-05T00:00:00Z","verified":true}`)
	})
	got, err := c.SSHKeys().Get(context.Background(), 5)
	if err != nil || got.UpdatedAt.IsZero() || !got.Verified {
		t.Errorf("key = %+v, %v", got, err)
	}
	org := (&orgJSON{Username: "acme"}).toOrg()
	if !org.CreatedAt.IsZero() {
		t.Errorf("org = %+v", org)
	}
}
