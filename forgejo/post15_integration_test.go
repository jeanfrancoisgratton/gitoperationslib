//go:build integration

package forgejo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

var post15Fields = Version{16, 0, 0}

// TestIntegrationAdminPost15Fields checks fields that only exist from 16 on, and
// that they stay zero below it.
func TestIntegrationAdminPost15Fields(t *testing.T) {
	ctx, c := adminEnv(t)
	has := c.Version().Compare(post15Fields) >= 0

	name := contracttest.RepoPrefix + "fields"
	org, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Orgs().Delete(context.Background(), name) })
	if org.CreatedAt.IsZero() == has {
		t.Errorf("org.CreatedAt = %v on %s", org.CreatedAt, c.Version())
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var blob bytes.Buffer
	for _, part := range [][]byte{[]byte("ssh-ed25519"), pub} {
		_ = binary.Write(&blob, binary.BigEndian, uint32(len(part)))
		blob.Write(part)
	}
	key, err := c.SSHKeys().Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: name, Key: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.SSHKeys().Delete(context.Background(), key.ID) })
	if key.UpdatedAt.IsZero() == has {
		t.Errorf("key.UpdatedAt = %v on %s", key.UpdatedAt, c.Version())
	}
}

func TestIntegrationAdminUserFilters(t *testing.T) {
	ctx, c := adminEnv(t)
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	logins := func(enabled bool) ([]string, error) {
		page, err := c.ListUsersBy(ctx, gitoperationslib.UserFilter{TwoFactor: &enabled}, gitoperationslib.ListOptions{PerPage: 50})
		var out []string
		for _, u := range page.Items {
			out = append(out, u.Login)
		}
		return out, err
	}
	without, err := logins(false)
	if c.Version().Compare(twoFactorFilterVersion) < 0 {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("before 16: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	with, err := logins(true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(without, admin) || slices.Contains(with, admin) {
		t.Errorf("with 2FA %v, without %v; admin %s has none", with, without, admin)
	}
	all, err := c.ListUsersBy(ctx, gitoperationslib.UserFilter{}, gitoperationslib.ListOptions{PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != len(without)+len(with) {
		t.Errorf("%d users, but %d with 2FA and %d without", len(all.Items), len(with), len(without))
	}
}

// TestIntegrationAdminAddMemberJoins checks the check: with invitations off, as on
// the disposable instance, a member really joins. Invitations need
// ADD_MEMBERS_BY_INVITATIONS, checked by hand on 17.0.1.
func TestIntegrationAdminAddMemberJoins(t *testing.T) {
	ctx, c := adminEnv(t)
	login := testLogin(t)
	cleanupUser(t, c, &login)
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{Login: login, Email: login + "@example.invalid", Password: "Passw0rd-long-1", MustChangePassword: gitoperationslib.Ptr(false)}); err != nil {
		t.Fatal(err)
	}
	name := contracttest.RepoPrefix + "joins"
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: name}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Orgs().Delete(context.Background(), name) })
	team, err := c.Teams().Create(ctx, name, gitoperationslib.CreateTeamOptions{Name: "members", Access: gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Teams().AddMember(ctx, team.ID, login); err != nil {
		t.Fatal(err)
	}
	if member, err := c.Teams().IsMember(ctx, team.ID, login); err != nil || !member {
		t.Errorf("IsMember = %v, %v", member, err)
	}
}
