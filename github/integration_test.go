//go:build integration

// Integration tests against github.com. They run only with -tags integration
// and these environment variables set:
//
//	GITOPSLIB_GITHUB_TOKEN  fine-grained token scoped to the test org
//	                        (Repository permissions: Administration read/write)
//	GITOPSLIB_GITHUB_ORG    throwaway organisation
//
// GITOPSLIB_GITHUB_COLLABORATOR, optional, is a user the collaborator checks
// may add to a test repository; GitHub sends them an invitation by e-mail.
//
// An org-scoped token cannot create repositories in the user's namespace, so
// those checks are skipped. Only repositories named with
// contracttest.RepoPrefix are created or deleted.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
	"github.com/jeanfrancoisgratton/gitoperationslib/webhook"
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
	return contracttest.Env{Client: c, Org: org, Login: login, CreateBranch: c.createBranch, WriteFile: c.writeFile, CheckPing: c.checkPingDelivery, PublicProtectionRepo: true, Collaborator: os.Getenv("GITOPSLIB_GITHUB_COLLABORATOR"), CreateEnvironment: c.createEnvironment}
}

func TestIntegrationCollaborators(t *testing.T) { contracttest.Collaborators(t, integrationEnv(t)) }

func TestIntegrationRepos(t *testing.T) {
	contracttest.Repos(t, integrationEnv(t))
}

func TestIntegrationReleases(t *testing.T) {
	contracttest.Releases(t, integrationEnv(t))
}

func TestIntegrationPullRequests(t *testing.T) {
	contracttest.PullRequests(t, integrationEnv(t))
}

func (c *Client) createEnvironment(ctx context.Context, owner, repo, name string) error {
	_, _, err := c.gh.Repositories.CreateUpdateEnvironment(ctx, owner, repo, name, &gh.CreateUpdateEnvironment{})
	return convertError(err)
}

// createBranch creates branch from the default branch with one commit adding
// file.
func (c *Client) createBranch(ctx context.Context, owner, repo, branch, file, content string) error {
	r, _, err := c.gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return convertError(err)
	}
	ref, _, err := c.gh.Git.GetRef(ctx, owner, repo, "heads/"+r.GetDefaultBranch())
	if err != nil {
		return convertError(err)
	}
	if _, _, err := c.gh.Git.CreateRef(ctx, owner, repo, gh.CreateRef{Ref: "refs/heads/" + branch, SHA: ref.GetObject().GetSHA()}); err != nil {
		return convertError(err)
	}
	_, _, err = c.gh.Repositories.CreateFile(ctx, owner, repo, file, &gh.RepositoryContentFileOptions{
		Message: gh.Ptr("Add " + file), Content: []byte(content), Branch: gh.Ptr(branch),
	})
	return convertError(err)
}

func TestIntegrationDiffs(t *testing.T) { contracttest.Diffs(t, integrationEnv(t)) }

// writeFile commits file to branch, creating or replacing it.
func (c *Client) writeFile(ctx context.Context, owner, repo, branch, file, content string) error {
	opts := &gh.RepositoryContentFileOptions{Message: gh.Ptr("Write " + file), Content: []byte(content), Branch: gh.Ptr(branch)}
	cur, _, _, err := c.gh.Repositories.GetContents(ctx, owner, repo, file, &gh.RepositoryContentGetOptions{Ref: branch})
	if err = convertError(err); err != nil && !gitoperationslib.IsNotFound(err) {
		return err
	}
	if err == nil {
		opts.SHA = cur.SHA
	}
	_, _, err = c.gh.Repositories.UpdateFile(ctx, owner, repo, file, opts)
	return convertError(err)
}

func TestIntegrationWebhooks(t *testing.T) {
	contracttest.Webhooks(t, integrationEnv(t))
}

// TestIntegrationHookDeliveries reads the delivery of a ping, with its request,
// and sends it again. The target does not exist, so deliveries simply fail.
func TestIntegrationHookDeliveries(t *testing.T) {
	env := integrationEnv(t)
	c := env.Client.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := contracttest.RepoPrefix + "deliveries"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), env.Org, name) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: env.Org, Name: name, AutoInit: true}); err != nil {
		t.Fatal(err)
	}
	scope := gitoperationslib.RepoHooks(env.Org, name)
	hook, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: contracttest.HookURL, Secret: "s3cret", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hook.Events, []gitoperationslib.HookEvent{gitoperationslib.HookEventAll}) {
		t.Errorf("Events = %v", hook.Events)
	}
	if err := c.Webhooks().Ping(ctx, scope, hook.ID); err != nil {
		t.Fatal(err)
	}
	find := func(redelivery bool) *gitoperationslib.HookDelivery {
		t.Helper()
		for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
			page, err := c.ListHookDeliveries(ctx, scope, hook.ID, gitoperationslib.ListHookDeliveriesOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for i := range page.Items {
				if d := &page.Items[i]; d.Event == "ping" && d.Redelivery == redelivery {
					return d
				}
			}
		}
		t.Fatalf("no ping delivery with Redelivery = %v", redelivery)
		return nil
	}
	first := find(false)
	if first.ID == 0 || first.GUID == "" || first.StatusCode == 0 || first.DeliveredAt.IsZero() || first.RequestPayload != nil {
		t.Errorf("listed = %+v", first)
	}
	full, err := c.GetHookDelivery(ctx, scope, hook.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	// As the delivery documentation says: compacted, it decodes and verifies.
	var body bytes.Buffer
	if err := json.Compact(&body, full.RequestPayload); err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	for k, v := range full.RequestHeaders {
		headers.Set(k, v)
	}
	if err := webhook.Verify(body.Bytes(), headers, []byte("s3cret")); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if _, err := c.GetHookDelivery(ctx, scope, hook.ID, 1); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(unknown) = %v", err)
	}
	if err := c.RedeliverHook(ctx, scope, hook.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if again := find(true); again.GUID != first.GUID || again.ID == first.ID {
		t.Errorf("redelivered = %+v, first %+v", again, first)
	}
}

// checkPingDelivery reads GitHub's record of the ping delivery (headers and
// payload as sent) and checks that the webhook package accepts it. The
// recorded payload is re-compacted, which reproduces the signed bytes.
func (c *Client) checkPingDelivery(t *testing.T, ctx context.Context, owner, repo string, hookID int64, secret []byte) {
	t.Helper()
	var ping *gh.HookDelivery
	for deadline := time.Now().Add(30 * time.Second); ping == nil && time.Now().Before(deadline); time.Sleep(2 * time.Second) {
		list, _, err := c.gh.Repositories.ListHookDeliveries(ctx, owner, repo, hookID, nil)
		if err != nil {
			t.Fatal(convertError(err))
		}
		for _, d := range list {
			if d.GetEvent() == "ping" {
				ping = d
				break
			}
		}
	}
	if ping == nil {
		t.Fatal("no ping delivery recorded")
	}
	d, _, err := c.gh.Repositories.GetHookDelivery(ctx, owner, repo, hookID, ping.GetID())
	if err != nil {
		t.Fatal(convertError(err))
	}
	var body bytes.Buffer
	if err := json.Compact(&body, *d.Request.RawPayload); err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	for k, v := range d.Request.Headers {
		h.Set(k, v)
	}
	if err := webhook.Verify(body.Bytes(), h, secret); err != nil {
		t.Fatalf("GitHub's ping delivery does not verify: %v", err)
	}
	ev, err := webhook.Decode(h, body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Forge != gitoperationslib.BackendGitHub || ev.Name != "ping" || ev.Repo != owner+"/"+repo {
		t.Errorf("decoded ping = %+v", ev)
	}
	t.Logf("verified GitHub ping delivery %s", ev.Delivery)
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

// TestIntegrationWorkflowRuns runs real workflows, which uses Actions minutes.
func TestIntegrationWorkflowRuns(t *testing.T) {
	env := integrationEnv(t)
	env.Workflows = &contracttest.WorkflowEnv{RunsOn: "ubuntu-latest", Dir: ".github/workflows", UploadArtifact: "actions/upload-artifact@v4"}
	contracttest.WorkflowRuns(t, env)
}

func TestIntegrationCommits(t *testing.T) { contracttest.Commits(t, integrationEnv(t)) }

// TestIntegrationRepoActionsSettings checks GitHub's Actions settings, which take
// their whole state: one change keeps the rest.
func TestIntegrationRepoActionsSettings(t *testing.T) {
	env := integrationEnv(t)
	c := env.Client.(*Client)
	ctx := context.Background()
	repos := c.Repos()
	created, err := repos.Create(ctx, gitoperationslib.CreateRepoOptions{Owner: env.Org, Name: contracttest.RepoPrefix + "actions-" + strconv.FormatInt(time.Now().UnixNano()%100000, 10), AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repos.Delete(context.Background(), created.Owner, created.Name) })
	got, err := repos.Get(ctx, created.Owner, created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Actions; a.Allowed == nil || a.TokenPermission == nil || a.ApprovePullRequests == nil {
		t.Fatalf("Actions settings = %+v", a)
	}
	write, local := gitoperationslib.ActionsTokenWrite, gitoperationslib.ActionsAllowedLocalOnly
	edited, err := repos.Edit(ctx, created.Owner, created.Name, gitoperationslib.EditRepoOptions{Actions: gitoperationslib.ActionsSettings{TokenPermission: &write, Allowed: &local}})
	if err != nil {
		t.Fatal(err)
	}
	if a := edited.Actions; *a.TokenPermission != write || *a.Allowed != local || *a.ApprovePullRequests != *got.Actions.ApprovePullRequests || !*edited.Features.Actions {
		t.Errorf("edited = %+v, features %+v", a, edited.Features)
	}
	// Turning Actions off and on keeps them.
	if _, err := repos.Edit(ctx, created.Owner, created.Name, gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Actions: gitoperationslib.Ptr(false)}}); err != nil {
		t.Fatal(err)
	}
	back, err := repos.Edit(ctx, created.Owner, created.Name, gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Actions: gitoperationslib.Ptr(true)}})
	if err != nil {
		t.Fatal(err)
	}
	if a := back.Actions; *a.Allowed != local || *a.TokenPermission != write {
		t.Errorf("after off and on = %+v", a)
	}
}
