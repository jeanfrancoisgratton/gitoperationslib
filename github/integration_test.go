//go:build integration

// Integration tests against github.com. They run only with -tags integration
// and these environment variables set:
//
//	GITOPSLIB_GITHUB_TOKEN  fine-grained token scoped to the test org
//	                        (Repository permissions: Administration read/write)
//	GITOPSLIB_GITHUB_ORG    throwaway organisation
//
// An org-scoped token cannot create repositories in the user's namespace, so
// those checks are skipped. Only repositories named with
// contracttest.RepoPrefix are created or deleted.
package github

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

func integrationEnv(t *testing.T) contracttest.Env {
	t.Helper()
	token, org := os.Getenv("GITOPSLIB_GITHUB_TOKEN"), os.Getenv("GITOPSLIB_GITHUB_ORG")
	if token == "" || org == "" {
		t.Skip("GITOPSLIB_GITHUB_TOKEN and GITOPSLIB_GITHUB_ORG must be set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := New(token)
	if err != nil {
		t.Fatal(err)
	}
	login, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GitHub user %s, org %s", login, org)
	return contracttest.Env{Client: c, Org: org, Login: login}
}

func TestIntegrationRepos(t *testing.T) {
	contracttest.Repos(t, integrationEnv(t))
}

func TestIntegrationReleases(t *testing.T) {
	contracttest.Releases(t, integrationEnv(t))
}
