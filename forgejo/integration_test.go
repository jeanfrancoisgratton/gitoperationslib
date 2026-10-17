//go:build integration

// Integration tests against a real Forgejo instance. They run only with
// -tags integration and these environment variables set:
//
//	GITOPSLIB_FORGEJO_URL    instance URL
//	GITOPSLIB_FORGEJO_TOKEN  token of a dedicated test user
//	GITOPSLIB_FORGEJO_ORG    throwaway organisation the test user owns
//
// Only repositories named with contracttest.RepoPrefix are created or deleted.
package forgejo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

func integrationEnv(t *testing.T) contracttest.Env {
	t.Helper()
	url, token, org := os.Getenv("GITOPSLIB_FORGEJO_URL"), os.Getenv("GITOPSLIB_FORGEJO_TOKEN"), os.Getenv("GITOPSLIB_FORGEJO_ORG")
	if url == "" || token == "" || org == "" {
		t.Skip("GITOPSLIB_FORGEJO_URL, GITOPSLIB_FORGEJO_TOKEN and GITOPSLIB_FORGEJO_ORG must be set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := New(ctx, url, Token(token))
	if err != nil {
		t.Fatal(err)
	}
	login, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Forgejo %s, user %s, org %s", c.Version(), login, org)
	return contracttest.Env{Client: c, Org: org, Login: login, UserRepos: true}
}

func TestIntegrationRepos(t *testing.T) {
	contracttest.Repos(t, integrationEnv(t))
}
