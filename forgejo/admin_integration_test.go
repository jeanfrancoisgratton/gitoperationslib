//go:build integration

// Admin integration tests. They need a site-admin token, so they run only
// against the disposable instance from scripts/forgejo-testenv.sh:
//
//	GITOPSLIB_TESTENV_URL          must point at localhost
//	GITOPSLIB_TESTENV_ADMIN_TOKEN  admin token created by the script
package forgejo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// adminEnv returns an admin client for the disposable test instance. It
// refuses any URL that is not a loopback address, so admin tests can never
// run against a real server.
func adminEnv(t *testing.T) (context.Context, *Client) {
	t.Helper()
	rawURL, token := os.Getenv("GITOPSLIB_TESTENV_URL"), os.Getenv("GITOPSLIB_TESTENV_ADMIN_TOKEN")
	if rawURL == "" || token == "" {
		t.Skip("GITOPSLIB_TESTENV_URL and GITOPSLIB_TESTENV_ADMIN_TOKEN must be set (scripts/forgejo-testenv.sh start)")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); !(host == "localhost" || (ip != nil && ip.IsLoopback())) {
		t.Fatalf("refusing to run admin tests against %q: only loopback test instances are allowed", rawURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	c, err := New(ctx, rawURL, Token(token))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, c
}

// testLogin returns a random login carrying the contract prefix, so leftovers
// are easy to recognise.
func testLogin(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return contracttest.RepoPrefix + "u" + hex.EncodeToString(b)
}

func cleanupUser(t *testing.T, c *Client, login *string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.Users().Delete(ctx, *login, gitoperationslib.DeleteUserOptions{Purge: true})
		if err != nil && !gitoperationslib.IsNotFound(err) {
			t.Errorf("cleanup user %s: %v", *login, err)
		}
	})
}

func TestIntegrationAdminUsers(t *testing.T) {
	ctx, c := adminEnv(t)
	users := c.Users()

	login := testLogin(t)
	cleanupUser(t, c, &login)

	created, err := users.Create(ctx, gitoperationslib.CreateUserOptions{
		Login: login, Email: login + "@example.invalid", Password: "Integration-Test-1234",
		FullName: "Integration Test", MustChangePassword: gitoperationslib.Ptr(false),
		Visibility: gitoperationslib.OwnerVisibilityLimited,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Login != login || created.FullName != "Integration Test" || created.Visibility != gitoperationslib.OwnerVisibilityLimited ||
		created.IsAdmin || !created.Active || !created.LastLogin.IsZero() || created.CreatedAt.IsZero() {
		t.Errorf("created = %+v", created)
	}

	if _, err := users.Create(ctx, gitoperationslib.CreateUserOptions{Login: login, Email: "other@example.invalid", Password: "Integration-Test-1234"}); gitoperationslib.StatusCode(err) != 422 {
		t.Errorf("duplicate create: err = %v, want HTTP 422", err)
	}

	got, err := users.Get(ctx, login)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	found := false
	for u, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, users.List) {
		if err != nil {
			t.Fatal(err)
		}
		found = found || u.Login == login
	}
	if !found {
		t.Error("List did not return the test user")
	}

	private := gitoperationslib.OwnerVisibilityPrivate
	edited, err := users.Edit(ctx, login, gitoperationslib.EditUserOptions{
		FullName: gitoperationslib.Ptr("Edited"), Location: gitoperationslib.Ptr("Montréal"), Visibility: &private,
		ProhibitLogin: gitoperationslib.Ptr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.FullName != "Edited" || edited.Location != "Montréal" || edited.Visibility != private || !edited.ProhibitLogin {
		t.Errorf("edited = %+v", edited)
	}

	// Forgejo 15.0.x ignores changes to public; Edit must report it rather
	// than succeed silently. Accept success too, in case a release fixes it.
	public := gitoperationslib.OwnerVisibilityPublic
	edited, err = users.Edit(ctx, login, gitoperationslib.EditUserOptions{Visibility: &public, Website: gitoperationslib.Ptr("https://example.com")})
	switch {
	case errors.Is(err, gitoperationslib.ErrNotSupported):
		t.Logf("visibility to public: %v", err)
		if edited == nil || edited.Website != "https://example.com" {
			t.Errorf("other changes should be applied and returned; got %+v", edited)
		}
	case err != nil:
		t.Fatal(err)
	case edited.Visibility != public:
		t.Errorf("Edit succeeded but visibility is %q", edited.Visibility)
	default:
		t.Log("server now honours visibility changes to public")
	}

	newLogin := login + "-r"
	if err := users.Rename(ctx, login, newLogin); err != nil {
		t.Fatal(err)
	}
	login = newLogin
	if renamed, err := users.Get(ctx, newLogin); err != nil || renamed.ID != created.ID {
		t.Fatalf("Get after rename = %+v, %v", renamed, err)
	}

	if err := users.Delete(ctx, login, gitoperationslib.DeleteUserOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Get(ctx, login); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

// TestIntegrationAdminDeleteNeedsPurge documents that deleting a user who
// owns repositories fails without Purge.
func TestIntegrationAdminDeleteNeedsPurge(t *testing.T) {
	ctx, c := adminEnv(t)
	login := testLogin(t)
	cleanupUser(t, c, &login)

	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{
		Login: login, Email: login + "@example.invalid", Password: "Integration-Test-1234", MustChangePassword: gitoperationslib.Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}
	// Admin creates a repository on the user's behalf.
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: login, Name: contracttest.RepoPrefix + "owned"}); err != nil {
		t.Fatal(err)
	}

	err := c.Users().Delete(ctx, login, gitoperationslib.DeleteUserOptions{})
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("delete without purge: err = %v, want an API error", err)
	}
	t.Logf("delete without purge: %v", err)

	if err := c.Users().Delete(ctx, login, gitoperationslib.DeleteUserOptions{Purge: true}); err != nil {
		t.Fatalf("delete with purge: %v", err)
	}
}
