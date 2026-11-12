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
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
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

// TestIntegrationPaging checks lists past what fits on a page, on the real paging
// of each endpoint: webhooks use tokens, commits numbers with a context token.
func TestIntegrationPaging(t *testing.T) {
	env := integrationEnv(t)
	c := env.Client.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	name := contracttest.RepoPrefix + "paging"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), env.Org, name) })
	created, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: env.Org, Name: name, AutoInit: true, Visibility: gitoperationslib.VisibilityPrivate})
	if err != nil {
		t.Fatal(err)
	}
	scope := gitoperationslib.RepoHooks(created.Owner, created.Name)
	const hooks = 12 // the default page holds 10
	for i := range hooks {
		if _, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
			URL: fmt.Sprintf("https://example.com/%d", i), Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}, Active: gitoperationslib.Ptr(false),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, perPage := range []int{0, 5} {
		seen := map[int64]bool{}
		for h, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: perPage}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
			return c.Webhooks().List(ctx, scope, o)
		}) {
			if err != nil {
				t.Fatal(err)
			}
			if seen[h.ID] {
				t.Errorf("webhook %d listed twice", h.ID)
			}
			seen[h.ID] = true
		}
		if len(seen) != hooks {
			t.Errorf("PerPage %d: listed %d webhooks, want %d", perPage, len(seen), hooks)
		}
	}

	if err := c.createBranch(ctx, created.Owner, created.Name, "paged", "first.txt", "x\n"); err != nil {
		t.Fatal(err)
	}
	const commits = 5
	for i := 1; i < commits; i++ {
		if err := c.writeFile(ctx, created.Owner, created.Name, "paged", fmt.Sprintf("f%d.txt", i), "x\n"); err != nil {
			t.Fatal(err)
		}
	}
	for _, opts := range []gitoperationslib.ListCommitsOptions{
		{Ref: "paged", ListOptions: gitoperationslib.ListOptions{PerPage: 2}, Exclude: created.DefaultBranch},
		{Ref: "paged", ListOptions: gitoperationslib.ListOptions{PerPage: 1}, Exclude: created.DefaultBranch},
	} {
		seen := map[string]bool{}
		for commit, err := range gitoperationslib.All(ctx, opts.ListOptions, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
			opts := opts
			opts.ListOptions = o
			return c.Commits().List(ctx, created.Owner, created.Name, opts)
		}) {
			if err != nil {
				t.Fatal(err)
			}
			if seen[commit.SHA] {
				t.Errorf("commit %s listed twice", commit.SHA)
			}
			seen[commit.SHA] = true
		}
		if len(seen) != commits {
			t.Errorf("PerPage %d: listed %d commits of the branch, want %d", opts.PerPage, len(seen), commits)
		}
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

func TestIntegrationRunners(t *testing.T) { contracttest.Runners(t, integrationEnv(t)) }

func TestIntegrationCommitStatuses(t *testing.T) { contracttest.CommitStatuses(t, integrationEnv(t)) }

func TestIntegrationBranchInfo(t *testing.T) { contracttest.BranchInfo(t, integrationEnv(t)) }
