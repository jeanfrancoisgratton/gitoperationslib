//go:build integration

package forgejo

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// TestIntegrationMirrors pushes a repository to another one on the same
// instance, then has a pull mirror fetch a new commit, on a real server.
func TestIntegrationMirrors(t *testing.T) {
	ctx, c := adminEnv(t)
	me, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, target, pull := testLogin(t), testLogin(t), testLogin(t)
	for _, name := range []string{source, target, pull} {
		t.Cleanup(func() { c.Repos().Delete(context.Background(), me, name) })
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: source, AutoInit: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: target}); err != nil {
		t.Fatal(err)
	}
	// The address as the server sees it, whatever port the test reaches it on.
	// Forgejo 17 refuses plain http remotes, and the test instance has no TLS.
	const local = "http://127.0.0.1:3000/"
	needsPlainHTTP := func(t *testing.T) {
		t.Helper()
		if v, err := ParseVersion("17.0.0"); err == nil && c.Version().Compare(v) >= 0 {
			t.Skipf("Forgejo %s refuses plain http remotes", c.Version())
		}
	}
	token := os.Getenv("GITOPSLIB_TESTENV_ADMIN_TOKEN")

	eventually := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); !cond(); time.Sleep(500 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}

	t.Run("not a mirror", func(t *testing.T) {
		if err := c.Mirrors().Sync(ctx, me, source); gitoperationslib.StatusCode(err) != http.StatusBadRequest {
			t.Errorf("SyncMirror = %v, want 400", err)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for name, opts := range map[string]gitoperationslib.CreatePushMirrorOptions{
			"short interval": {RemoteAddress: "https://example.com/x.git", Interval: time.Minute},
			"host":           {RemoteAddress: "https://gitopslib.invalid/x.git", Interval: time.Hour},
			"credentials":    {RemoteAddress: "https://u:p@example.com/x.git", Interval: time.Hour},
		} {
			if _, err := c.Mirrors().CreatePush(ctx, me, source, opts); gitoperationslib.StatusCode(err) != http.StatusBadRequest {
				t.Errorf("%s: err = %v, want 400", name, err)
			} else {
				t.Logf("%s: %v", name, err)
			}
		}
	})

	t.Run("ssh", func(t *testing.T) {
		m, err := c.Mirrors().CreatePush(ctx, me, source, gitoperationslib.CreatePushMirrorOptions{RemoteAddress: "git@example.com:x/y.git", UseSSH: true})
		if err != nil {
			t.Fatal(err)
		}
		if m.PublicKey == "" || m.Interval != 0 {
			t.Errorf("mirror = %+v", m)
		}
		if err := c.Mirrors().DeletePush(ctx, me, source, m.Name); err != nil {
			t.Error(err)
		}
	})

	t.Run("push", func(t *testing.T) {
		needsPlainHTTP(t)
		m, err := c.Mirrors().CreatePush(ctx, me, source, gitoperationslib.CreatePushMirrorOptions{
			RemoteAddress: local + me + "/" + target + ".git", Username: me, Password: token,
			Interval: 8 * time.Hour, SyncOnCommit: true, BranchFilter: "main",
		})
		if err != nil {
			t.Fatal(err)
		}
		if m.Name == "" || m.Interval != 8*time.Hour || !m.SyncOnCommit || m.BranchFilter != "main" || !m.LastSync.IsZero() || m.CreatedAt.IsZero() {
			t.Errorf("mirror = %+v", m)
		}
		other, err := c.Mirrors().CreatePush(ctx, me, source, gitoperationslib.CreatePushMirrorOptions{RemoteAddress: "https://example.com/x/y.git", Interval: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		// Paging, as both fit on a page otherwise.
		var names []string
		for item, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.PushMirror], error) {
			return c.Mirrors().ListPush(ctx, me, source, o)
		}) {
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, item.Name)
		}
		if len(names) != 2 || names[0] == names[1] {
			t.Errorf("listed %v", names)
		}
		if err := c.Mirrors().DeletePush(ctx, me, source, other.Name); err != nil {
			t.Error(err)
		}

		if err := c.Mirrors().SyncPush(ctx, me, source); err != nil {
			t.Fatal(err)
		}
		eventually("the push", func() bool {
			got, err := c.Mirrors().GetPush(ctx, me, source, m.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got.LastError != "" {
				t.Fatalf("LastError = %q", got.LastError)
			}
			return !got.LastSync.IsZero()
		})
		eventually("the branch on the target", func() bool {
			_, err := c.Branches().Get(ctx, me, target, "main")
			return err == nil
		})

		if err := c.Mirrors().DeletePush(ctx, me, source, m.Name); err != nil {
			t.Error(err)
		}
		if err := c.Mirrors().DeletePush(ctx, me, source, m.Name); !gitoperationslib.IsNotFound(err) {
			t.Errorf("second delete = %v", err)
		}
		if _, err := c.Mirrors().GetPush(ctx, me, source, m.Name); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Get after delete = %v", err)
		}
	})

	// A push that fails stops the sync: the mirrors after it are not reached.
	t.Run("failing", func(t *testing.T) {
		needsPlainHTTP(t)
		good := func() *gitoperationslib.PushMirror {
			t.Helper()
			m, err := c.Mirrors().CreatePush(ctx, me, source, gitoperationslib.CreatePushMirrorOptions{
				RemoteAddress: local + me + "/" + target + ".git", Username: me, Password: token, Interval: time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			return m
		}
		first := good()
		bad, err := c.Mirrors().CreatePush(ctx, me, source, gitoperationslib.CreatePushMirrorOptions{
			RemoteAddress: local + me + "/" + target + ".git", Username: me, Password: "wrong", Interval: time.Hour,
		})
		if err != nil {
			t.Fatal(err) // nothing is contacted yet
		}
		last := good()
		for _, m := range []*gitoperationslib.PushMirror{first, bad, last} {
			t.Cleanup(func() { c.Mirrors().DeletePush(context.Background(), me, source, m.Name) })
		}

		err = c.Mirrors().SyncPush(ctx, me, source)
		if gitoperationslib.StatusCode(err) != http.StatusInternalServerError || !strings.Contains(err.Error(), bad.Name) {
			t.Errorf("SyncPushMirrors = %v, want 500 naming %s", err, bad.Name)
		}
		get := func(name string) *gitoperationslib.PushMirror {
			t.Helper()
			m, err := c.Mirrors().GetPush(ctx, me, source, name)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}
		if m := get(first.Name); m.LastSync.IsZero() || m.LastError != "" {
			t.Errorf("first = %+v", m)
		}
		if m := get(bad.Name); m.LastSync.IsZero() || m.LastError == "" {
			t.Errorf("failing = %+v", m)
		}
		if m := get(last.Name); !m.LastSync.IsZero() {
			t.Errorf("last = %+v, should not have been pushed", m)
		}
	})

	t.Run("pull", func(t *testing.T) {
		needsPlainHTTP(t)
		// The library has no migration yet.
		body := map[string]any{
			"clone_addr": local + me + "/" + source + ".git", "auth_token": token, "repo_name": pull, "mirror": true, "service": "git",
		}
		if _, err := c.api.Do(ctx, http.MethodPost, "repos/migrate", nil, body, nil); err != nil {
			t.Fatal(err)
		}
		repo, err := c.Repos().Get(ctx, me, pull)
		if err != nil {
			t.Fatal(err)
		}
		if !repo.Mirror {
			t.Errorf("Mirror = false")
		}
		if err := c.writeFile(ctx, me, source, "main", "new.txt", "new\n"); err != nil {
			t.Fatal(err)
		}
		head, err := c.Branches().Get(ctx, me, source, "main")
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Mirrors().Sync(ctx, me, pull); err != nil {
			t.Fatal(err)
		}
		eventually("the fetch", func() bool {
			b, err := c.Branches().Get(ctx, me, pull, "main")
			return err == nil && b.SHA == head.SHA
		})
	})
}
