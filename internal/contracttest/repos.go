// Package contracttest holds behaviour checks shared by every backend's
// integration tests, so all backends are held to the same contract.
package contracttest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// RepoPrefix starts the name of every repository the checks create. Cleanup
// refuses to delete anything without it.
const RepoPrefix = "gitopslib-it-"

// Env describes the backend under test.
type Env struct {
	Client gitoperationslib.Client
	Org    string // throwaway organisation the authenticated user owns
	Login  string // authenticated user
	// UserRepos enables checks that create repositories in the user's own
	// namespace (not possible with an org-scoped GitHub token).
	UserRepos bool
}

// Repos runs the repository checks.
func Repos(t *testing.T, env Env) {
	t.Run("OrgLifecycle", func(t *testing.T) { orgRepoLifecycle(t, env) })
	t.Run("UserRepos", func(t *testing.T) {
		if !env.UserRepos {
			t.Skip("user namespace checks disabled for this backend")
		}
		userRepos(t, env)
	})
	t.Run("ListAccessible", func(t *testing.T) { listAccessible(t, env) })
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func repoName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return RepoPrefix + hex.EncodeToString(b)
}

// retryConflict retries fn while it fails with HTTP 409: GitHub returns it
// while a just-created repository is still being initialised.
func retryConflict(ctx context.Context, t *testing.T, fn func() error) error {
	for delay := time.Second; ; delay *= 2 {
		err := fn()
		if gitoperationslib.StatusCode(err) != http.StatusConflict || delay > 16*time.Second {
			return err
		}
		t.Logf("conflict, retrying in %s: %v", delay, err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
	}
}

// eventually polls cond for up to 15 seconds, for endpoints that are only
// eventually consistent.
func eventually(ctx context.Context, cond func() bool) bool {
	deadline := time.Now().Add(15 * time.Second)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
}

// cleanup deletes owner/*name at the end of the test; name is a pointer so
// renames are followed.
func cleanup(t *testing.T, c gitoperationslib.Client, owner string, name *string) {
	t.Cleanup(func() {
		if !strings.HasPrefix(*name, RepoPrefix) {
			t.Errorf("refusing to delete %s/%s: missing %q prefix", owner, *name, RepoPrefix)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := retryConflict(ctx, t, func() error { return c.Repos().Delete(ctx, owner, *name) })
		if err != nil && !gitoperationslib.IsNotFound(err) {
			t.Errorf("cleanup %s/%s: %v", owner, *name, err)
		}
	})
}

// create creates a private repository and registers its cleanup.
func create(t *testing.T, ctx context.Context, env Env, opts gitoperationslib.CreateRepoOptions) (*gitoperationslib.Repo, *string) {
	t.Helper()
	name := repoName(t)
	opts.Name = name
	if opts.Visibility == "" {
		opts.Visibility = gitoperationslib.VisibilityPrivate
	}
	owner := opts.Owner
	if owner == "" {
		owner = env.Login
	}
	cleanup(t, env.Client, owner, &name)
	repo, err := env.Client.Repos().Create(ctx, opts)
	if err != nil {
		t.Fatalf("create in %q: %v", opts.Owner, err)
	}
	return repo, &name
}

func orgRepoLifecycle(t *testing.T, env Env) {
	ctx := testContext(t)
	repos := env.Client.Repos()

	created, name := create(t, ctx, env, gitoperationslib.CreateRepoOptions{
		Owner: env.Org, Description: "gitoperationslib integration test",
		AutoInit: true, Gitignores: []string{"Go"}, License: "MIT",
	})
	// Fatal, so cleanup runs at once if a test repo was exposed publicly.
	if created.Visibility != gitoperationslib.VisibilityPrivate {
		t.Fatalf("created with visibility %q, want private", created.Visibility)
	}
	if !strings.EqualFold(created.Owner, env.Org) || created.Name != *name {
		t.Errorf("created = %+v", created)
	}
	_, second := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org})

	got, err := repos.Get(ctx, env.Org, *name)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.FullName != env.Org+"/"+*name || got.DefaultBranch == "" ||
		got.CloneURL == "" || got.HTMLURL == "" || got.CreatedAt.IsZero() {
		t.Errorf("got = %+v", got)
	}

	// PerPage 1 forces one request per repository.
	found := map[string]bool{}
	fetch := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return repos.List(ctx, env.Org, o)
	}
	for repo, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		found[repo.Name] = true
	}
	if !found[*name] || !found[*second] {
		t.Errorf("List did not return both test repos; found %v", found)
	}

	newName := *name + "-renamed"
	var edited *gitoperationslib.Repo
	err = retryConflict(ctx, t, func() (err error) {
		edited, err = repos.Edit(ctx, env.Org, *name, gitoperationslib.EditRepoOptions{
			Name: &newName, Description: gitoperationslib.Ptr("edited"), Website: gitoperationslib.Ptr("https://example.com"),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	*name = newName
	if edited.Name != newName || edited.Description != "edited" || edited.Website != "https://example.com" {
		t.Errorf("edited = %+v", edited)
	}

	if err := retryConflict(ctx, t, func() error { return repos.Delete(ctx, env.Org, newName) }); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Get(ctx, env.Org, newName); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

func userRepos(t *testing.T, env Env) {
	ctx := testContext(t)
	for _, owner := range []string{"", env.Login} {
		created, _ := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: owner})
		if !strings.EqualFold(created.Owner, env.Login) {
			t.Errorf("owner %q: created in %q, want %q", owner, created.Owner, env.Login)
		}
	}
}

// listAccessible checks that an empty-owner List includes repositories of
// organisations the user belongs to.
func listAccessible(t *testing.T, env Env) {
	ctx := testContext(t)
	_, name := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org})
	fetch := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return env.Client.Repos().List(ctx, "", o)
	}
	for repo, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 50}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		if repo.Owner == env.Org && repo.Name == *name {
			return
		}
	}
	t.Errorf("List(\"\") did not include %s/%s", env.Org, *name)
}
