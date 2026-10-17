//go:build integration

// Integration tests against a real Forgejo instance. They run only with
// -tags integration and these environment variables set:
//
//	GITOPSLIB_FORGEJO_URL    instance URL
//	GITOPSLIB_FORGEJO_TOKEN  token of a dedicated test user
//	GITOPSLIB_FORGEJO_ORG    throwaway organisation the test user owns
//
// Every repository created is named with testRepoPrefix, and cleanup only
// deletes repositories carrying that prefix.
package forgejo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const testRepoPrefix = "gitopslib-it-"

type itEnv struct {
	client *Client
	org    string
	login  string
}

func integrationEnv(t *testing.T) (context.Context, itEnv) {
	t.Helper()
	url, token, org := os.Getenv("GITOPSLIB_FORGEJO_URL"), os.Getenv("GITOPSLIB_FORGEJO_TOKEN"), os.Getenv("GITOPSLIB_FORGEJO_ORG")
	if url == "" || token == "" || org == "" {
		t.Skip("GITOPSLIB_FORGEJO_URL, GITOPSLIB_FORGEJO_TOKEN and GITOPSLIB_FORGEJO_ORG must be set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	c, err := New(ctx, url, Token(token))
	if err != nil {
		t.Fatal(err)
	}
	login, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Forgejo %s, user %s, org %s", c.Version(), login, org)
	return ctx, itEnv{client: c, org: org, login: login}
}

func testRepoName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return testRepoPrefix + hex.EncodeToString(b)
}

// cleanupRepo deletes owner/*name at the end of the test; name is a pointer
// so renames are followed.
func cleanupRepo(t *testing.T, c *Client, owner string, name *string) {
	t.Cleanup(func() {
		if !strings.HasPrefix(*name, testRepoPrefix) {
			t.Errorf("refusing to delete %s/%s: missing %q prefix", owner, *name, testRepoPrefix)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Repos().Delete(ctx, owner, *name); err != nil && !gitoperationslib.IsNotFound(err) {
			t.Errorf("cleanup %s/%s: %v", owner, *name, err)
		}
	})
}

func TestIntegrationOrgRepoLifecycle(t *testing.T) {
	ctx, env := integrationEnv(t)
	repos := env.client.Repos()

	names := []string{testRepoName(t), testRepoName(t)}
	for i := range names {
		cleanupRepo(t, env.client, env.org, &names[i])
	}

	created, err := repos.Create(ctx, gitoperationslib.CreateRepoOptions{
		Owner: env.org, Name: names[0], Description: "gitoperationslib integration test",
		Visibility: gitoperationslib.VisibilityPrivate, AutoInit: true, DefaultBranch: "main",
		Gitignores: []string{"Go"}, License: "MIT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Owner != env.org || created.Name != names[0] || created.Visibility != gitoperationslib.VisibilityPrivate {
		t.Errorf("created = %+v", created)
	}
	if _, err := repos.Create(ctx, gitoperationslib.CreateRepoOptions{Owner: env.org, Name: names[1], Visibility: gitoperationslib.VisibilityPrivate}); err != nil {
		t.Fatal(err)
	}

	got, err := repos.Get(ctx, env.org, names[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.DefaultBranch != "main" || got.Empty || got.FullName != env.org+"/"+names[0] ||
		got.CloneURL == "" || got.CreatedAt.IsZero() {
		t.Errorf("got = %+v", got)
	}

	// PerPage 1 forces one request per repository.
	found := map[string]bool{}
	fetch := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return repos.List(ctx, env.org, o)
	}
	for repo, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		found[repo.Name] = true
	}
	if !found[names[0]] || !found[names[1]] {
		t.Errorf("List did not return both test repos; found %v", found)
	}

	newName := names[0] + "-renamed"
	edited, err := repos.Edit(ctx, env.org, names[0], gitoperationslib.EditRepoOptions{
		Name: &newName, Description: gitoperationslib.Ptr("edited"), Website: gitoperationslib.Ptr("https://example.com"),
	})
	if err != nil {
		t.Fatal(err)
	}
	names[0] = newName
	if edited.Name != newName || edited.Description != "edited" || edited.Website != "https://example.com" {
		t.Errorf("edited = %+v", edited)
	}

	if err := repos.Delete(ctx, env.org, newName); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Get(ctx, env.org, newName); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

func TestIntegrationUserRepos(t *testing.T) {
	ctx, env := integrationEnv(t)
	repos := env.client.Repos()

	for _, owner := range []string{"", env.login} {
		name := testRepoName(t)
		cleanupRepo(t, env.client, env.login, &name)

		created, err := repos.Create(ctx, gitoperationslib.CreateRepoOptions{Owner: owner, Name: name, Visibility: gitoperationslib.VisibilityPrivate})
		if err != nil {
			t.Fatalf("owner %q: %v", owner, err)
		}
		if created.Owner != env.login {
			t.Errorf("owner %q: created in %q, want %q", owner, created.Owner, env.login)
		}
	}

	// Record whether an empty-owner List includes repositories of orgs the
	// user belongs to, to document its semantics.
	orgRepo := testRepoName(t)
	cleanupRepo(t, env.client, env.org, &orgRepo)
	if _, err := repos.Create(ctx, gitoperationslib.CreateRepoOptions{Owner: env.org, Name: orgRepo, Visibility: gitoperationslib.VisibilityPrivate}); err != nil {
		t.Fatal(err)
	}
	page, err := repos.List(ctx, "", gitoperationslib.ListOptions{PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]int{}
	for _, r := range page.Items {
		owners[r.Owner]++
	}
	t.Logf("List(\"\") owners: %v", owners)
}
