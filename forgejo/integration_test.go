//go:build integration

// Integration tests against a real Forgejo instance. They run only with
// -tags integration and these environment variables set:
//
//	GITOPSLIB_FORGEJO_URL    instance URL
//	GITOPSLIB_FORGEJO_TOKEN  token of a dedicated test user
//	GITOPSLIB_FORGEJO_ORG    throwaway organisation the test user owns
//
// GITOPSLIB_FORGEJO_COLLABORATOR, optional, is a user the collaborator checks
// may add to a test repository.
//
// Only repositories named with contracttest.RepoPrefix are created or deleted.
package forgejo

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
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
	return contracttest.Env{Client: c, Org: org, Login: login, UserRepos: true, CreateBranch: c.createBranch, WriteFile: c.writeFile, Collaborator: os.Getenv("GITOPSLIB_FORGEJO_COLLABORATOR")}
}

func TestIntegrationCollaborators(t *testing.T) { contracttest.Collaborators(t, integrationEnv(t)) }

func TestIntegrationDiffs(t *testing.T) {
	contracttest.Diffs(t, integrationEnv(t))
}

// writeFile commits file to branch, creating or replacing it.
func (c *Client) writeFile(ctx context.Context, owner, repo, branch, file, content string) error {
	path := forgejoapi.Path("repos", owner, repo, "contents", file)
	body := map[string]string{
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"message": "Write " + file,
		"branch":  branch,
	}
	var cur struct {
		SHA string `json:"sha"`
	}
	_, err := c.api.Do(ctx, http.MethodGet, path, url.Values{"ref": {branch}}, nil, &cur)
	switch {
	case gitoperationslib.IsNotFound(err):
		_, err = c.api.Do(ctx, http.MethodPost, path, nil, body, nil)
	case err == nil:
		body["sha"] = cur.SHA
		_, err = c.api.Do(ctx, http.MethodPut, path, nil, body, nil)
	}
	return err
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

// TestIntegrationWorkflowRuns needs GITOPSLIB_TESTENV_RUNNER=1 on the disposable
// instance, and that instance as GITOPSLIB_FORGEJO_URL.
func TestIntegrationWorkflowRuns(t *testing.T) {
	label := os.Getenv("GITOPSLIB_TESTENV_RUNNER_LABEL")
	if label == "" {
		t.Skip("GITOPSLIB_TESTENV_RUNNER_LABEL is not set: no runner")
	}
	env := integrationEnv(t)
	c := env.Client.(*Client)
	env.Workflows = &contracttest.WorkflowEnv{
		RunsOn: label, Dir: ".forgejo/workflows", UploadArtifact: "https://data.forgejo.org/forgejo/upload-artifact@v3",
		Prepare: func(ctx context.Context, owner, repo string) error {
			_, err := c.api.Do(ctx, http.MethodPatch, repoPath(owner, repo), nil, map[string]bool{"has_actions": true}, nil)
			return err
		},
	}
	contracttest.WorkflowRuns(t, env)
}

func TestIntegrationCommits(t *testing.T) { contracttest.Commits(t, integrationEnv(t)) }
