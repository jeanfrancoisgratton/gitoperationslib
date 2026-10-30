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
	"encoding/base64"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
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
	return contracttest.Env{Client: c, Org: org, Login: login, UserRepos: true, CreateBranch: c.createBranch}
}

func TestIntegrationRepos(t *testing.T) {
	contracttest.Repos(t, integrationEnv(t))
}

func TestIntegrationReleases(t *testing.T) {
	contracttest.Releases(t, integrationEnv(t))
}

func TestIntegrationPullRequests(t *testing.T) {
	contracttest.PullRequests(t, integrationEnv(t))
}

// createBranch creates branch from the default branch with one commit adding
// file, through the contents API.
func (c *Client) createBranch(ctx context.Context, owner, repo, branch, file, content string) error {
	body := map[string]string{
		"content":    base64.StdEncoding.EncodeToString([]byte(content)),
		"message":    "Add " + file,
		"new_branch": branch,
	}
	_, err := c.api.Do(ctx, http.MethodPost, forgejoapi.Path("repos", owner, repo, "contents", file), nil, body, nil)
	return err
}

func TestIntegrationWebhooks(t *testing.T) {
	contracttest.Webhooks(t, integrationEnv(t))
}

func TestIntegrationBranchesAndTags(t *testing.T) {
	contracttest.BranchesAndTags(t, integrationEnv(t))
}

func TestIntegrationProtections(t *testing.T) {
	contracttest.Protections(t, integrationEnv(t))
}

func TestIntegrationIssues(t *testing.T) {
	contracttest.Issues(t, integrationEnv(t))
}

func TestIntegrationDirectory(t *testing.T) { contracttest.Directory(t, integrationEnv(t)) }

func TestIntegrationAccount(t *testing.T) { contracttest.Account(t, integrationEnv(t)) }

func TestIntegrationCI(t *testing.T) { contracttest.CI(t, integrationEnv(t)) }

func TestIntegrationCommits(t *testing.T) { contracttest.Commits(t, integrationEnv(t)) }
