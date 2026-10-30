//go:build integration

// Admin integration tests. They need a site-admin token, so they run only
// against the disposable instance from scripts/forgejo-testenv.sh:
//
//	GITOPSLIB_TESTENV_URL          must point at localhost
//	GITOPSLIB_TESTENV_ADMIN_TOKEN  admin token created by the script
package forgejo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/webhook"
)

// adminEnv returns an admin client for the disposable test instance. It
// refuses any URL that is not a loopback address, so admin tests can never
// run against a real server.
func adminEnv(t *testing.T) (context.Context, *Client) {
	t.Helper()
	rawURL, token := os.Getenv("GITOPSLIB_TESTENV_URL"), os.Getenv("GITOPSLIB_TESTENV_ADMIN_TOKEN")
	if rawURL == "" || token == "" {
		t.Skip("GITOPSLIB_TESTENV_URL and GITOPSLIB_TESTENV_ADMIN_TOKEN must be set (scripts/forgejo-testenv.sh start)")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); !(host == "localhost" || (ip != nil && ip.IsLoopback())) {
		t.Fatalf("refusing to run admin tests against %q: only loopback test instances are allowed", rawURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	c, err := New(ctx, rawURL, Token(token))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, c
}

// testLogin returns a random login carrying the contract prefix, so leftovers
// are easy to recognise.
func testLogin(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return contracttest.RepoPrefix + "u" + hex.EncodeToString(b)
}

func cleanupUser(t *testing.T, c *Client, login *string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.Users().Delete(ctx, *login, gitoperationslib.DeleteUserOptions{Purge: true})
		if err != nil && !gitoperationslib.IsNotFound(err) {
			t.Errorf("cleanup user %s: %v", *login, err)
		}
	})
}

func TestIntegrationAdminUsers(t *testing.T) {
	ctx, c := adminEnv(t)
	users := c.Users()

	login := testLogin(t)
	cleanupUser(t, c, &login)

	created, err := users.Create(ctx, gitoperationslib.CreateUserOptions{
		Login: login, Email: login + "@example.invalid", Password: "Integration-Test-1234",
		FullName: "Integration Test", MustChangePassword: gitoperationslib.Ptr(false),
		Visibility: gitoperationslib.OwnerVisibilityLimited,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Login != login || created.FullName != "Integration Test" || created.Visibility != gitoperationslib.OwnerVisibilityLimited ||
		created.IsAdmin || !created.Active || !created.LastLogin.IsZero() || created.CreatedAt.IsZero() {
		t.Errorf("created = %+v", created)
	}

	if _, err := users.Create(ctx, gitoperationslib.CreateUserOptions{Login: login, Email: "other@example.invalid", Password: "Integration-Test-1234"}); gitoperationslib.StatusCode(err) != 422 {
		t.Errorf("duplicate create: err = %v, want HTTP 422", err)
	}

	got, err := users.Get(ctx, login)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	found := false
	for u, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, users.List) {
		if err != nil {
			t.Fatal(err)
		}
		found = found || u.Login == login
	}
	if !found {
		t.Error("List did not return the test user")
	}

	private := gitoperationslib.OwnerVisibilityPrivate
	edited, err := users.Edit(ctx, login, gitoperationslib.EditUserOptions{
		FullName: gitoperationslib.Ptr("Edited"), Location: gitoperationslib.Ptr("Montréal"), Visibility: &private,
		ProhibitLogin: gitoperationslib.Ptr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.FullName != "Edited" || edited.Location != "Montréal" || edited.Visibility != private || !edited.ProhibitLogin {
		t.Errorf("edited = %+v", edited)
	}

	// Forgejo 15.0.x ignores changes to public; Edit must report it rather
	// than succeed silently. Accept success too, in case a release fixes it.
	public := gitoperationslib.OwnerVisibilityPublic
	edited, err = users.Edit(ctx, login, gitoperationslib.EditUserOptions{Visibility: &public, Website: gitoperationslib.Ptr("https://example.com")})
	switch {
	case errors.Is(err, gitoperationslib.ErrNotSupported):
		t.Logf("visibility to public: %v", err)
		if edited == nil || edited.Website != "https://example.com" {
			t.Errorf("other changes should be applied and returned; got %+v", edited)
		}
	case err != nil:
		t.Fatal(err)
	case edited.Visibility != public:
		t.Errorf("Edit succeeded but visibility is %q", edited.Visibility)
	default:
		t.Log("server now honours visibility changes to public")
	}

	newLogin := login + "-r"
	if err := users.Rename(ctx, login, newLogin); err != nil {
		t.Fatal(err)
	}
	login = newLogin
	if renamed, err := users.Get(ctx, newLogin); err != nil || renamed.ID != created.ID {
		t.Fatalf("Get after rename = %+v, %v", renamed, err)
	}

	if err := users.Delete(ctx, login, gitoperationslib.DeleteUserOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Get(ctx, login); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

// TestIntegrationAdminDeleteNeedsPurge documents that deleting a user who
// owns repositories fails without Purge.
func TestIntegrationAdminDeleteNeedsPurge(t *testing.T) {
	ctx, c := adminEnv(t)
	login := testLogin(t)
	cleanupUser(t, c, &login)

	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{
		Login: login, Email: login + "@example.invalid", Password: "Integration-Test-1234", MustChangePassword: gitoperationslib.Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}
	// Admin creates a repository on the user's behalf.
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: login, Name: contracttest.RepoPrefix + "owned"}); err != nil {
		t.Fatal(err)
	}

	err := c.Users().Delete(ctx, login, gitoperationslib.DeleteUserOptions{})
	if !errors.Is(err, gitoperationslib.ErrOwnsRepos) {
		t.Fatalf("delete without purge: err = %v, want ErrOwnsRepos", err)
	}

	if err := c.Users().Delete(ctx, login, gitoperationslib.DeleteUserOptions{Purge: true}); err != nil {
		t.Fatalf("delete with purge: %v", err)
	}
}

// cleanupOrg deletes the organisation *name and its repositories at the end
// of the test. Only used against the disposable instance.
func cleanupOrg(t *testing.T, c *Client, name *string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for repo, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
			return c.Repos().List(ctx, *name, o)
		}) {
			if gitoperationslib.IsNotFound(err) {
				return
			}
			if err != nil {
				t.Errorf("cleanup org %s: %v", *name, err)
				return
			}
			if err := c.Repos().Delete(ctx, *name, repo.Name); err != nil {
				t.Errorf("cleanup repo %s/%s: %v", *name, repo.Name, err)
			}
		}
		if err := c.Orgs().Delete(ctx, *name); err != nil && !gitoperationslib.IsNotFound(err) {
			t.Errorf("cleanup org %s: %v", *name, err)
		}
	})
}

func contains[T any](t *testing.T, ctx context.Context, fetch func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[T], error), match func(T) bool) bool {
	t.Helper()
	for item, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		if match(item) {
			return true
		}
	}
	return false
}

func TestIntegrationAdminOrgs(t *testing.T) {
	ctx, c := adminEnv(t)
	orgs := c.Orgs()
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Registered first, so it runs after the org cleanups.
	user := testLogin(t)
	cleanupUser(t, c, &user)
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{
		Login: user, Email: user + "@example.invalid", Password: "Integration-Test-1234", MustChangePassword: gitoperationslib.Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}

	own, theirs := testLogin(t)+"-o", testLogin(t)+"-o"
	cleanupOrg(t, c, &own)
	cleanupOrg(t, c, &theirs)

	created, err := orgs.Create(ctx, gitoperationslib.CreateOrgOptions{
		Name: own, FullName: "Integration Org", Description: "d", Visibility: gitoperationslib.OwnerVisibilityPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != own || created.FullName != "Integration Org" || created.Visibility != gitoperationslib.OwnerVisibilityPrivate {
		t.Errorf("created = %+v", created)
	}
	if _, err := orgs.Create(ctx, gitoperationslib.CreateOrgOptions{Name: theirs, Owner: user}); err != nil {
		t.Fatal(err)
	}

	if got, err := orgs.Get(ctx, own); err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	byName := func(name string) func(gitoperationslib.Org) bool {
		return func(o gitoperationslib.Org) bool { return o.Name == name }
	}
	if !contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return orgs.List(ctx, "", o)
	}, byName(own)) {
		t.Error("List(\"\") is missing the admin's org")
	}
	if !contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return orgs.List(ctx, user, o)
	}, byName(theirs)) {
		t.Error("List(user) is missing the org created for the user")
	}
	for _, name := range []string{own, theirs} {
		if !contains(t, ctx, orgs.ListAll, byName(name)) {
			t.Errorf("ListAll is missing %s", name)
		}
	}

	// Unlike users, organisations can be switched back to public.
	public := gitoperationslib.OwnerVisibilityPublic
	edited, err := orgs.Edit(ctx, own, gitoperationslib.EditOrgOptions{Description: gitoperationslib.Ptr("edited"), Visibility: &public})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Description != "edited" || edited.Visibility != public {
		t.Errorf("edited = %+v", edited)
	}

	if !contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return orgs.Members(ctx, theirs, o)
	}, func(u gitoperationslib.User) bool { return u.Login == user }) {
		t.Error("Members is missing the owner")
	}
	for _, tt := range []struct {
		org, user string
		want      bool
	}{{theirs, user, true}, {theirs, admin, false}, {own, admin, true}, {own, "gitopslib-no-such-user", false}} {
		if got, err := orgs.IsMember(ctx, tt.org, tt.user); err != nil || got != tt.want {
			t.Errorf("IsMember(%s, %s) = %v, %v; want %v", tt.org, tt.user, got, err, tt.want)
		}
	}
	if err := orgs.RemoveMember(ctx, theirs, user); err == nil {
		t.Error("removing the last owner succeeded")
	} else if !errors.Is(err, gitoperationslib.ErrLastOwner) {
		t.Errorf("remove last owner: err = %v, want ErrLastOwner", err)
	}

	// Deleting an organisation that owns repositories fails.
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: own, Name: contracttest.RepoPrefix + "org-owned"}); err != nil {
		t.Fatal(err)
	}
	if err := orgs.Delete(ctx, own); err == nil {
		t.Fatal("deleting an org that owns repositories succeeded")
	} else if !errors.Is(err, gitoperationslib.ErrOwnsRepos) {
		t.Errorf("delete org with repos: err = %v, want ErrOwnsRepos", err)
	}
	if err := c.Repos().Delete(ctx, own, contracttest.RepoPrefix+"org-owned"); err != nil {
		t.Fatal(err)
	}

	renamed := own + "-r"
	if err := orgs.Rename(ctx, own, renamed); err != nil {
		t.Fatal(err)
	}
	own = renamed
	if err := orgs.Delete(ctx, own); err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.Get(ctx, own); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

func TestIntegrationAdminTeams(t *testing.T) {
	ctx, c := adminEnv(t)
	teams := c.Teams()

	user := testLogin(t)
	cleanupUser(t, c, &user)
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{
		Login: user, Email: user + "@example.invalid", Password: "Integration-Test-1234", MustChangePassword: gitoperationslib.Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}
	org := testLogin(t) + "-o"
	cleanupOrg(t, c, &org)
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: org}); err != nil {
		t.Fatal(err)
	}

	// The owner team is protected.
	owners, err := teams.GetByName(ctx, org, "owners")
	if err != nil {
		t.Fatal(err)
	}
	if owners.Permission != gitoperationslib.TeamPermissionOwner || owners.Org != org {
		t.Errorf("owners = %+v", owners)
	}
	if err := teams.Delete(ctx, owners.ID); !errors.Is(err, gitoperationslib.ErrOwnerTeam) {
		t.Fatalf("delete owner team: err = %v, want ErrOwnerTeam", err)
	}
	if _, err := teams.Get(ctx, owners.ID); err != nil {
		t.Fatalf("owner team gone after refused delete: %v", err)
	}
	if _, err := teams.GetByName(ctx, org, "no-such-team"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("GetByName missing: err = %v, want 404", err)
	}

	units := map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{
		gitoperationslib.TeamUnitCode:   gitoperationslib.TeamPermissionWrite,
		gitoperationslib.TeamUnitIssues: gitoperationslib.TeamPermissionRead,
	}
	devs, err := teams.Create(ctx, org, gitoperationslib.CreateTeamOptions{
		Name: "devs", Description: "integration", Access: gitoperationslib.TeamAccess{Units: units}, CanCreateOrgRepo: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if devs.Units[gitoperationslib.TeamUnitCode] != gitoperationslib.TeamPermissionWrite ||
		devs.Units[gitoperationslib.TeamUnitIssues] != gitoperationslib.TeamPermissionRead || !devs.CanCreateOrgRepo {
		t.Errorf("devs = %+v", devs)
	}

	writers, err := teams.Create(ctx, org, gitoperationslib.CreateTeamOptions{Name: "writers", Access: gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionWrite}})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range gitoperationslib.AllTeamUnits {
		if writers.Units[u] != gitoperationslib.TeamPermissionWrite {
			t.Errorf("write shorthand: %s = %q", u, writers.Units[u])
		}
	}

	// Raise to admin, then lower back to per-unit access.
	raised, err := teams.Edit(ctx, devs.ID, gitoperationslib.EditTeamOptions{Access: &gitoperationslib.TeamAccess{Permission: gitoperationslib.TeamPermissionAdmin}})
	if err != nil {
		t.Fatal(err)
	}
	if raised.Permission != gitoperationslib.TeamPermissionAdmin {
		t.Errorf("raised = %+v", raised)
	}
	lowered, err := teams.Edit(ctx, devs.ID, gitoperationslib.EditTeamOptions{Access: &gitoperationslib.TeamAccess{Units: map[gitoperationslib.TeamUnit]gitoperationslib.TeamPermission{
		gitoperationslib.TeamUnitCode: gitoperationslib.TeamPermissionRead,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if lowered.Permission == gitoperationslib.TeamPermissionAdmin || lowered.Units[gitoperationslib.TeamUnitCode] != gitoperationslib.TeamPermissionRead {
		t.Errorf("lowered = %+v", lowered)
	}
	described, err := teams.Edit(ctx, devs.ID, gitoperationslib.EditTeamOptions{Name: gitoperationslib.Ptr("developers"), Description: gitoperationslib.Ptr("edited")})
	if err != nil {
		t.Fatal(err)
	}
	if described.Name != "developers" || described.Description != "edited" || described.Units[gitoperationslib.TeamUnitCode] != gitoperationslib.TeamPermissionRead {
		t.Errorf("described = %+v (access should be unchanged)", described)
	}

	if !contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
		return teams.List(ctx, org, o)
	}, func(tm gitoperationslib.Team) bool { return tm.ID == devs.ID }) {
		t.Error("List is missing the team")
	}

	// Membership through the team also makes the user an org member.
	if err := teams.AddMember(ctx, devs.ID, user); err != nil {
		t.Fatal(err)
	}
	if ok, err := teams.IsMember(ctx, devs.ID, user); err != nil || !ok {
		t.Errorf("team IsMember = %v, %v", ok, err)
	}
	if ok, err := c.Orgs().IsMember(ctx, org, user); err != nil || !ok {
		t.Errorf("org IsMember after team add = %v, %v", ok, err)
	}
	if !contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
		return teams.Members(ctx, devs.ID, o)
	}, func(u gitoperationslib.User) bool { return u.Login == user }) {
		t.Error("Members is missing the user")
	}

	repo := contracttest.RepoPrefix + "team"
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: org, Name: repo}); err != nil {
		t.Fatal(err)
	}
	if err := teams.AddRepo(ctx, devs.ID, org, repo); err != nil {
		t.Fatal(err)
	}
	hasRepo := func() bool {
		return contains(t, ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
			return teams.Repos(ctx, devs.ID, o)
		}, func(r gitoperationslib.Repo) bool { return r.Name == repo })
	}
	if !hasRepo() {
		t.Error("Repos is missing the added repo")
	}
	if err := teams.RemoveRepo(ctx, devs.ID, org, repo); err != nil {
		t.Fatal(err)
	}
	if hasRepo() {
		t.Error("repo still listed after RemoveRepo")
	}

	if err := teams.RemoveMember(ctx, devs.ID, user); err != nil {
		t.Fatal(err)
	}
	if ok, err := teams.IsMember(ctx, devs.ID, user); err != nil || ok {
		t.Errorf("IsMember after remove = %v, %v", ok, err)
	}

	if err := teams.Delete(ctx, devs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := teams.Get(ctx, devs.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}

// TestIntegrationAdminPullRequestReviews covers what the contract checks
// cannot do with one account: review requests and approvals by another user.
// The reviewer signs in with a password, which also exercises BasicAuth.
func TestIntegrationAdminPullRequestReviews(t *testing.T) {
	ctx, c := adminEnv(t)
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	reviewer, password := testLogin(t), "Integration-Test-1234"
	cleanupUser(t, c, &reviewer)
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{
		Login: reviewer, Email: reviewer + "@example.invalid", Password: password, MustChangePassword: gitoperationslib.Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + "reviews"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), admin, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo, AutoInit: true, Visibility: gitoperationslib.VisibilityPrivate}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.Do(ctx, http.MethodPut, forgejoapi.Path("repos", admin, repo, "collaborators", reviewer), nil,
		map[string]string{"permission": "write"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.createBranch(ctx, admin, repo, "feature", "feature.txt", "x\n"); err != nil {
		t.Fatal(err)
	}
	base, err := c.Repos().Get(ctx, admin, repo)
	if err != nil {
		t.Fatal(err)
	}
	prs := c.PullRequests()
	pr, err := prs.Create(ctx, admin, repo, gitoperationslib.CreatePROptions{Title: "Needs review", Head: "feature", Base: base.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}

	hasState := func(state gitoperationslib.ReviewState) bool {
		page, err := prs.ListReviews(ctx, admin, repo, pr.Number, gitoperationslib.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Items {
			if r.Reviewer == reviewer && r.State == state {
				return true
			}
		}
		return false
	}

	if err := prs.RequestReviewers(ctx, admin, repo, pr.Number, []string{reviewer}, nil); err != nil {
		t.Fatal(err)
	}
	if !hasState(gitoperationslib.ReviewRequested) {
		t.Error("review request not listed")
	}
	if err := prs.RemoveReviewers(ctx, admin, repo, pr.Number, []string{reviewer}, nil); err != nil {
		t.Fatal(err)
	}
	if hasState(gitoperationslib.ReviewRequested) {
		t.Error("review request still listed after RemoveReviewers")
	}

	rc, err := New(ctx, os.Getenv("GITOPSLIB_TESTENV_URL"), BasicAuth(reviewer, password))
	if err != nil {
		t.Fatal(err)
	}
	changes, err := rc.PullRequests().CreateReview(ctx, admin, repo, pr.Number, gitoperationslib.CreateReviewOptions{
		Event: gitoperationslib.ReviewEventRequestChanges, Body: "Please fix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changes.State != gitoperationslib.ReviewChangesRequested || changes.Reviewer != reviewer {
		t.Errorf("changes = %+v", changes)
	}
	approval, err := rc.PullRequests().CreateReview(ctx, admin, repo, pr.Number, gitoperationslib.CreateReviewOptions{
		Event: gitoperationslib.ReviewEventApprove, Body: "Thanks", CommitID: pr.Head.SHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.State != gitoperationslib.ReviewApproved || approval.CommitID != pr.Head.SHA || approval.SubmittedAt.IsZero() {
		t.Errorf("approval = %+v", approval)
	}
	if !hasState(gitoperationslib.ReviewApproved) {
		t.Error("approval not listed")
	}

	if err := prs.Merge(ctx, admin, repo, pr.Number, gitoperationslib.MergeOptions{Method: gitoperationslib.MergeMethodSquash, DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
}

// hookReceiver collects deliveries made to a webhook.Handler that the test
// container can reach (GITOPSLIB_TESTENV_HOOK_HOST, the Docker bridge
// gateway).
type hookReceiver struct {
	srv    *httptest.Server
	events chan received
}

type received struct {
	path string
	ev   *webhook.Event
}

func newHookReceiver(t *testing.T, secrets map[string][]byte) *hookReceiver {
	t.Helper()
	host := os.Getenv("GITOPSLIB_TESTENV_HOOK_HOST")
	if host == "" {
		t.Skip("GITOPSLIB_TESTENV_HOOK_HOST must be set (scripts/forgejo-testenv.sh start)")
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", host, err)
	}
	r := &hookReceiver{events: make(chan received, 100)}
	mux := http.NewServeMux()
	for path, secret := range secrets {
		mux.Handle(path, webhook.Handler(secret, func(_ context.Context, ev *webhook.Event) error {
			r.events <- received{path, ev}
			return nil
		}))
	}
	r.srv = &httptest.Server{Listener: l, Config: &http.Server{Handler: mux}}
	r.srv.Start()
	t.Cleanup(r.srv.Close)
	return r
}

// wait returns the first delivery to path with the given event name.
func (r *hookReceiver) wait(t *testing.T, path, name string) *webhook.Event {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case got := <-r.events:
			t.Logf("received %s on %s (action %q)", got.ev.Name, got.path, got.ev.Action)
			if got.path == path && got.ev.Name == name {
				return got.ev
			}
		case <-timeout:
			t.Fatalf("no %s delivery on %s", name, path)
		}
	}
}

func TestIntegrationAdminWebhookDeliveries(t *testing.T) {
	ctx, c := adminEnv(t)
	repoSecret := []byte("repo-secret")
	recv := newHookReceiver(t, map[string][]byte{"/repo": repoSecret})
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	repo := contracttest.RepoPrefix + "hooks"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), admin, repo) })
	created, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo, AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}

	scope := gitoperationslib.RepoHooks(admin, repo)
	hook, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: recv.srv.URL + "/repo", Secret: string(repoSecret),
		Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequest, gitoperationslib.HookEventRelease},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Forgejo's test delivery is a push of the latest commit.
	if err := c.Webhooks().Ping(ctx, scope, hook.ID); err != nil {
		t.Fatal(err)
	}
	ping := recv.wait(t, "/repo", "push")
	if ping.Forge != gitoperationslib.BackendForgejo || ping.Repo != admin+"/"+repo || ping.Delivery == "" {
		t.Errorf("ping = %+v", ping)
	}

	if err := c.createBranch(ctx, admin, repo, "feature", "feature.txt", "x\n"); err != nil {
		t.Fatal(err)
	}
	push, err := recv.wait(t, "/repo", "push").Push()
	if err != nil {
		t.Fatal(err)
	}
	if push.Branch() != "feature" || push.Pusher != admin || len(push.Commits) != 1 || push.Commits[0].Message != "Add feature.txt\n" && push.Commits[0].Message != "Add feature.txt" {
		t.Errorf("push = %+v", push)
	}

	pr, err := c.PullRequests().Create(ctx, admin, repo, gitoperationslib.CreatePROptions{Title: "Hooked", Head: "feature", Base: created.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	prEvent, err := recv.wait(t, "/repo", "pull_request").PullRequest()
	if err != nil {
		t.Fatal(err)
	}
	if prEvent.Action != "opened" || prEvent.Number != pr.Number || prEvent.PullRequest.Head.Ref != "feature" || prEvent.PullRequest.Title != "Hooked" {
		t.Errorf("pull request event = %+v", prEvent)
	}

	if _, err := c.Releases().Create(ctx, admin, repo, gitoperationslib.CreateReleaseOptions{TagName: "v1.0.0", Name: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	rel, err := recv.wait(t, "/repo", "release").Release()
	if err != nil {
		t.Fatal(err)
	}
	if rel.Action != "published" || rel.Release.TagName != "v1.0.0" || rel.Release.Name != "1.0.0" {
		t.Errorf("release event = %+v", rel)
	}
}
