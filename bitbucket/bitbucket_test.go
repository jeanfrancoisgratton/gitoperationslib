package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fakeBitbucket routes requests to handler after answering the account and
// workspace lookups shared by most calls.
func fakeBitbucket(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2.0/user", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"uuid":"{me}","nickname":"alice","account_id":"70121:me"}`)
	})
	mux.HandleFunc("GET /2.0/user/workspaces", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"values":[{"workspace":{"slug":"acme"}}]}`)
	})
	mux.HandleFunc("GET /2.0/workspaces/acme/members", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"values":[{"user":{"uuid":"{alice}","nickname":"alice","account_id":"70121:a"}},{"user":{"uuid":"{bob}","nickname":"bob","account_id":"70121:b"}}]}`)
	})
	mux.HandleFunc("/", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(Token("tok"), WithBaseURL(srv.URL+"/2.0/"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCredentials(t *testing.T) {
	for name, tc := range map[string]struct {
		creds Credentials
		want  string
	}{
		"access token": {Token("ATCTTx"), "Bearer ATCTTx"},
		"API token":    {APIToken("alice@example.com", "ATATTx"), "Basic YWxpY2VAZXhhbXBsZS5jb206QVRBVFR4"},
		"basic":        {BasicAuth("alice", "pw"), "Basic YWxpY2U6cHc="},
	} {
		r, _ := http.NewRequest(http.MethodGet, "https://api.bitbucket.org/2.0/user", nil)
		tc.creds.apply(r)
		if got := r.Header.Get("Authorization"); got != tc.want {
			t.Errorf("%s: Authorization = %q, want %q", name, got, tc.want)
		}
	}
}

func body(r *http.Request) string {
	data, _ := io.ReadAll(r.Body)
	return strings.TrimSpace(string(data))
}

func TestCreateRepoWithAutoInit(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/2.0/repositories/acme/widget/src":
			r.ParseForm()
			requests = append(requests, "SRC branch="+r.Form.Get("branch")+" readme="+fmt.Sprint(r.Form.Get("README.md") != ""))
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost:
			requests = append(requests, r.Method+" "+r.URL.Path+" "+body(r))
			io.WriteString(w, `{"slug":"widget","full_name":"acme/widget","is_private":true,"mainbranch":{"name":"master"}}`)
		default:
			io.WriteString(w, `{"slug":"widget","full_name":"acme/widget","is_private":true,"mainbranch":{"name":"trunk"},
				"links":{"html":{"href":"https://bitbucket.org/acme/widget"},"clone":[{"name":"https","href":"https://bitbucket.org/acme/widget.git"},{"name":"ssh","href":"git@bitbucket.org:acme/widget.git"}]}}`)
		}
	})
	repo, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{
		Name: "Widget", Visibility: gitoperationslib.VisibilityPrivate, AutoInit: true, DefaultBranch: "trunk",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /2.0/repositories/acme/widget {"scm":"git","name":"Widget","is_private":true}`,
		"SRC branch=trunk readme=true",
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}
	if repo.Owner != "acme" || repo.Name != "widget" || repo.DefaultBranch != "trunk" || repo.Visibility != gitoperationslib.VisibilityPrivate ||
		repo.CloneURL == "" || repo.SSHURL == "" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestUnsupportedOptions(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"gitignore": func() error {
			_, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", Gitignores: []string{"Go"}})
			return err
		},
		"internal": func() error {
			_, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", Visibility: gitoperationslib.VisibilityInternal})
			return err
		},
		"branch, no init": func() error {
			_, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", DefaultBranch: "main"})
			return err
		},
		"archive": func() error {
			_, err := c.Repos().Edit(ctx, "acme", "x", gitoperationslib.EditRepoOptions{Archived: gitoperationslib.Ptr(true)})
			return err
		},
		"branch rename": func() error { _, err := c.Branches().Rename(ctx, "acme", "x", "a", "b"); return err },
		"fork PR": func() error {
			_, err := c.PullRequests().Create(ctx, "acme", "x", gitoperationslib.CreatePROptions{Title: "t", Head: "u:b", Base: "main"})
			return err
		},
		"manual merge": func() error {
			return c.PullRequests().Merge(ctx, "acme", "x", 1, gitoperationslib.MergeOptions{Method: gitoperationslib.MergeMethodManuallyMerged})
		},
		"auto-merge": func() error {
			return c.PullRequests().Merge(ctx, "acme", "x", 1, gitoperationslib.MergeOptions{WhenChecksSucceed: true})
		},
		"team reviewers": func() error { return c.PullRequests().RequestReviewers(ctx, "acme", "x", 1, nil, []string{"devs"}) },
		"commit review": func() error {
			_, err := c.PullRequests().CreateReview(ctx, "acme", "x", 1, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove, CommitID: "abc"})
			return err
		},
		"named checks": func() error {
			_, err := c.BranchProtections().Create(ctx, "acme", "x", gitoperationslib.BranchProtection{Pattern: "main", RequiredStatusChecks: []string{"ci"}})
			return err
		},
		"form hooks": func() error {
			_, err := c.Webhooks().Create(ctx, gitoperationslib.RepoHooks("acme", "x"), gitoperationslib.CreateWebhookOptions{URL: "https://h", Events: []gitoperationslib.HookEvent{"push"}, ContentType: gitoperationslib.HookContentForm})
			return err
		},
		"release event": func() error {
			_, err := c.Webhooks().Create(ctx, gitoperationslib.RepoHooks("acme", "x"), gitoperationslib.CreateWebhookOptions{URL: "https://h", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventRelease}})
			return err
		},
		"ping": func() error { return c.Webhooks().Ping(ctx, gitoperationslib.RepoHooks("acme", "x"), 1) },
		"releases": func() error {
			_, err := c.Releases().Create(ctx, "acme", "x", gitoperationslib.CreateReleaseOptions{TagName: "v1"})
			return err
		},
		"issues":         func() error { _, err := c.Issues().Get(ctx, "acme", "x", 1); return err },
		"labels":         func() error { return c.Labels().Delete(ctx, "acme", "x", "bug") },
		"milestones":     func() error { _, err := c.Milestones().Get(ctx, "acme", "x", 1); return err },
		"tag protection": func() error { _, err := c.TagProtections().List(ctx, "acme", "x"); return err },
		"directory":      func() error { _, err := c.Directory().OrgInfo(ctx, "acme"); return err },
	} {
		if err := call(); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
}

func TestPullRequestsBitbucket(t *testing.T) {
	var writes []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method+" "+r.URL.Path+" "+body(r))
		}
		switch {
		case r.URL.Path == "/2.0/repositories/acme/widget/pullrequests" && r.Method == http.MethodGet:
			if fmt.Sprint(r.URL.Query()["state"]) != "[MERGED DECLINED SUPERSEDED]" {
				t.Errorf("state filter = %v", r.URL.Query()["state"])
			}
			io.WriteString(w, `{"values":[{"id":3,"state":"DECLINED"}]}`)
		case strings.HasSuffix(r.URL.Path, "/merge"):
			io.WriteString(w, `{"id":3,"state":"MERGED"}`)
		default:
			io.WriteString(w, `{"id":3,"title":"T","state":"OPEN","author":{"nickname":"alice"},
				"source":{"branch":{"name":"feat"},"commit":{"hash":"abcdef123456"}},"destination":{"branch":{"name":"main"}},
				"reviewers":[{"uuid":"{bob}"}],
				"participants":[{"user":{"nickname":"bob"},"role":"REVIEWER","approved":true,"participated_on":"2026-10-20T10:00:00+00:00"},
				                {"user":{"nickname":"carol"},"role":"PARTICIPANT","approved":false,"state":"changes_requested"},
				                {"user":{"nickname":"dave"},"role":"PARTICIPANT","approved":false,"state":null}]}`)
		}
	})
	ctx := context.Background()
	prs := c.PullRequests()

	page, err := prs.List(ctx, "acme", "widget", gitoperationslib.ListPROptions{State: gitoperationslib.PRStateClosed})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != gitoperationslib.PRStateClosed {
		t.Fatalf("List = %+v, %v", page, err)
	}
	// Head check passes with the full SHA of the 12-character head.
	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{
		Method: gitoperationslib.MergeMethodRebase, HeadSHA: "abcdef1234567890", DeleteBranch: true, Title: "T", Message: "M",
	}); err != nil {
		t.Fatal(err)
	}
	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{HeadSHA: "999999"}); gitoperationslib.StatusCode(err) != http.StatusConflict {
		t.Errorf("moved head: err = %v, want 409", err)
	}
	reviews, err := prs.ListReviews(ctx, "acme", "widget", 3, gitoperationslib.ListOptions{})
	if err != nil || len(reviews.Items) != 2 || reviews.Items[0].State != gitoperationslib.ReviewApproved ||
		reviews.Items[1].State != gitoperationslib.ReviewChangesRequested || reviews.Items[1].Reviewer != "carol" {
		t.Errorf("ListReviews = %+v, %v", reviews, err)
	}
	if err := prs.RequestReviewers(ctx, "acme", "widget", 3, []string{"alice"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(true), State: gitoperationslib.Ptr(gitoperationslib.PRStateClosed)}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /2.0/repositories/acme/widget/pullrequests/3/merge {"close_source_branch":true,"merge_strategy":"rebase_fast_forward","message":"T\n\nM"}`,
		`PUT /2.0/repositories/acme/widget/pullrequests/3 {"reviewers":[{"uuid":"{bob}"},{"uuid":"{alice}"}],"title":"T"}`,
		`PUT /2.0/repositories/acme/widget/pullrequests/3 {"draft":true}`,
		`POST /2.0/repositories/acme/widget/pullrequests/3/decline `,
	}
	// Reviewer order comes from a map; normalise it.
	for i, w := range writes {
		if strings.Contains(w, `"reviewers"`) && strings.Contains(w, `{"uuid":"{alice}"},{"uuid":"{bob}"}`) {
			writes[i] = strings.Replace(w, `{"uuid":"{alice}"},{"uuid":"{bob}"}`, `{"uuid":"{bob}"},{"uuid":"{alice}"}`, 1)
		}
	}
	if strings.Join(writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes:\n%s\nwant:\n%s", strings.Join(writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestHookEvents(t *testing.T) {
	keys, err := toEventKeys([]gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequestReview, "repo:commit_status_updated"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	back := fromEventKeys(append(keys, "pullrequest:created"))
	if fmt.Sprint(back) != "[push pull_request_review repo:commit_status_updated pullrequest:created]" {
		t.Errorf("fromEventKeys = %v (an incomplete pull_request group stays raw)", back)
	}
	if id := hookID("{0b6a4b8e-1234-4000-8000-000000000000}"); id != 0x0b6a4b8e1234400 {
		t.Errorf("hookID = %x", id)
	}
}

func TestWebhooksBitbucket(t *testing.T) {
	const uuid = "{0b6a4b8e-1234-4000-8000-000000000000}"
	var writes []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method+" "+r.URL.EscapedPath()+" "+body(r))
		}
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"values":[{"uuid":%q,"url":"https://h","active":true,"events":["repo:push"]}]}`, uuid)
		default:
			fmt.Fprintf(w, `{"uuid":%q,"url":"https://h2","active":false,"events":["repo:fork"]}`, uuid)
		}
	})
	ctx := context.Background()
	hooks := c.Webhooks()
	scope := gitoperationslib.OrgHooks("acme")
	h, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{URL: "https://h", Secret: "s", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.Edit(ctx, scope, h.ID, gitoperationslib.EditWebhookOptions{URL: gitoperationslib.Ptr("https://h2"), Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventFork}}); err != nil {
		t.Fatal(err)
	}
	if err := hooks.Delete(ctx, scope, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.Get(ctx, scope, 42); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown ID: err = %v", err)
	}
	want := []string{
		`POST /2.0/workspaces/acme/hooks {"url":"https://h","description":"gitoperationslib","active":true,"events":["repo:push"],"secret":"s"}`,
		`PUT /2.0/workspaces/acme/hooks/%7B0b6a4b8e-1234-4000-8000-000000000000%7D {"url":"https://h2","description":"","active":true,"events":["repo:fork"]}`,
		`DELETE /2.0/workspaces/acme/hooks/%7B0b6a4b8e-1234-4000-8000-000000000000%7D `,
	}
	if strings.Join(writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes:\n%s\nwant:\n%s", strings.Join(writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestBranchProtectionBitbucket(t *testing.T) {
	var posted []restrictionJSON
	failKind := ""
	var deleted []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var rj restrictionJSON
			json.NewDecoder(r.Body).Decode(&rj)
			if rj.Kind == failKind {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"type":"error","error":{"message":"premium merge checks cannot be enabled without a premium plan"}}`)
				return
			}
			rj.ID = int64(len(posted) + 1)
			posted = append(posted, rj)
			json.NewEncoder(w).Encode(rj)
		case http.MethodDelete:
			deleted = append(deleted, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			w.WriteHeader(http.StatusNoContent)
		default:
			io.WriteString(w, `{"values":[
				{"id":1,"kind":"require_approvals_to_merge","pattern":"main","branch_match_kind":"glob","value":2},
				{"id":2,"kind":"delete","pattern":"main","branch_match_kind":"glob"},
				{"id":3,"kind":"push","pattern":"main","branch_match_kind":"glob","users":[{"nickname":"alice"}],"groups":[{"slug":"devs"}]},
				{"id":4,"kind":"force","pattern":"release/*","branch_match_kind":"glob"},
				{"id":5,"kind":"delete","pattern":"x","branch_match_kind":"branching_model"}]}`)
		}
	})
	ctx := context.Background()
	bp := c.BranchProtections()

	rule, err := bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{
		Pattern: "main", RequiredApprovals: 2, PushAllowlist: &gitoperationslib.Allowlist{Users: []string{"alice"}, Teams: []string{"devs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, p := range posted {
		kinds = append(kinds, p.Kind)
	}
	if fmt.Sprint(kinds) != "[require_approvals_to_merge delete force push]" || posted[3].Users[0].UUID != "{alice}" || posted[3].Groups[0].Slug != "devs" {
		t.Errorf("posted = %+v", posted)
	}
	if rule.RequiredApprovals != 2 || rule.AllowDeletion || !rule.AllowForcePush || !rule.ApplyToAdmins ||
		rule.PushAllowlist == nil || fmt.Sprint(rule.PushAllowlist.Users, rule.PushAllowlist.Teams) != "[alice] [devs]" {
		t.Errorf("rule = %+v", rule)
	}
	rules, err := bp.List(ctx, "acme", "widget")
	if err != nil || len(rules) != 2 || rules[1].Pattern != "release/*" || rules[1].AllowForcePush {
		t.Errorf("List = %+v, %v (branching-model rules are skipped)", rules, err)
	}

	// A failing restriction rolls back the ones already created.
	posted, failKind = nil, kindResetStale
	_, err = bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{Pattern: "dev", RequiredApprovals: 1, DismissStaleApprovals: true})
	if err == nil || !strings.Contains(err.Error(), "premium") {
		t.Errorf("err = %v", err)
	}
	if !slices.Equal(deleted, []string{"1"}) {
		t.Errorf("rolled back %v, want [1]", deleted)
	}
}
