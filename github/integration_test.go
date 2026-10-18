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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
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
	return contracttest.Env{Client: c, Org: org, Login: login, CreateBranch: c.createBranch, CheckPing: c.checkPingDelivery}
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

func TestIntegrationWebhooks(t *testing.T) {
	contracttest.Webhooks(t, integrationEnv(t))
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
