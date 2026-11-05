//go:build integration

// Integration tests against Bitbucket Cloud. They run only with
// -tags integration and these environment variables set:
//
//	GITOPSLIB_BITBUCKET_TOKEN      Atlassian API token with Bitbucket scopes
//	                               (ATATT...), or an access token (ATCTT...)
//	GITOPSLIB_BITBUCKET_EMAIL      Atlassian account e-mail; required with an
//	                               API token (basic auth), unset with an
//	                               access token (bearer)
//	GITOPSLIB_BITBUCKET_WORKSPACE  workspace to test in (optional when the
//	                               token can only see one)
//
// GITOPSLIB_BITBUCKET_COLLABORATOR, optional, is a user the collaborator checks
// may add to a test repository. The account's own {UUID} works: a workspace owner
// keeps access to the repository.
//
// Only repositories named with contracttest.RepoPrefix are created or deleted.
package bitbucket

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

func integrationEnv(t *testing.T) contracttest.Env {
	t.Helper()
	token := os.Getenv("GITOPSLIB_BITBUCKET_TOKEN")
	if token == "" {
		t.Skip("GITOPSLIB_BITBUCKET_TOKEN must be set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	creds := Token(token)
	if email := os.Getenv("GITOPSLIB_BITBUCKET_EMAIL"); email != "" {
		creds = APIToken(email, token)
	}
	c, err := New(creds)
	if err != nil {
		t.Fatal(err)
	}
	me, err := c.currentUser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspace := os.Getenv("GITOPSLIB_BITBUCKET_WORKSPACE")
	if workspace == "" {
		if workspace, err = c.defaultWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Bitbucket user %s, workspace %s", me.login(), workspace)
	return contracttest.Env{
		Client: c, Org: workspace, Login: me.login(), CreateBranch: c.createBranch, WriteFile: c.writeFile,
		HookURL:      "https://example.com/gitopslib-hook", // must resolve in DNS
		Collaborator: os.Getenv("GITOPSLIB_BITBUCKET_COLLABORATOR"),
		CreateEnvironment: func(ctx context.Context, owner, repo, name string) error {
			body := map[string]any{
				"type": "deployment_environment", "name": name,
				"environment_type": map[string]string{"type": "deployment_environment_type", "name": "Production"},
			}
			_, err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "environments"), nil, body, nil)
			return err
		},
	}
}

// createBranch creates branch from the default branch with one commit adding
// file, through the /src endpoint.
func (c *Client) createBranch(ctx context.Context, owner, repo, branch, file, content string) error {
	parent, err := c.resolveCommit(ctx, owner, repo, "")
	if err != nil {
		return err
	}
	form := url.Values{file: {content}, "message": {"Add " + file}, "branch": {branch}, "parents": {parent}}
	_, err = c.api.PostForm(ctx, repoPath(owner, repo, "src"), form, nil)
	return err
}

// writeFile commits file to branch, creating or replacing it.
func (c *Client) writeFile(ctx context.Context, owner, repo, branch, file, content string) error {
	form := url.Values{file: {content}, "message": {"Write " + file}, "branch": {branch}}
	_, err := c.api.PostForm(ctx, repoPath(owner, repo, "src"), form, nil)
	return err
}

func TestIntegrationCollaborators(t *testing.T) { contracttest.Collaborators(t, integrationEnv(t)) }

func TestIntegrationDiffs(t *testing.T) { contracttest.Diffs(t, integrationEnv(t)) }

func TestIntegrationRepos(t *testing.T) { contracttest.Repos(t, integrationEnv(t)) }

func TestIntegrationBranchesAndTags(t *testing.T) {
	contracttest.BranchesAndTags(t, integrationEnv(t))
}

func TestIntegrationProtections(t *testing.T) { contracttest.Protections(t, integrationEnv(t)) }

func TestIntegrationPullRequests(t *testing.T) { contracttest.PullRequests(t, integrationEnv(t)) }

func TestIntegrationWebhooks(t *testing.T) { contracttest.Webhooks(t, integrationEnv(t)) }

func TestIntegrationReleases(t *testing.T) { contracttest.Releases(t, integrationEnv(t)) }

func TestIntegrationIssues(t *testing.T) { contracttest.Issues(t, integrationEnv(t)) }

func TestIntegrationDirectory(t *testing.T) { contracttest.Directory(t, integrationEnv(t)) }

func TestIntegrationAccount(t *testing.T) { contracttest.Account(t, integrationEnv(t)) }

func TestIntegrationCI(t *testing.T) { contracttest.CI(t, integrationEnv(t)) }

func TestIntegrationCommits(t *testing.T) { contracttest.Commits(t, integrationEnv(t)) }
