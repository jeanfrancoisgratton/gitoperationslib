package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestUserSecretsListByVersion(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		io.WriteString(w, `[{"name":"TOKEN","created_at":"2026-11-05T00:00:00Z"}]`)
	})
	ctx := context.Background()
	if _, err := c.Secrets().List(ctx, gitoperationslib.UserCI(), gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "17.0.0") {
		t.Errorf("before 17: %v", err)
	}
	c.version = Version{17, 0, 1}
	page, err := c.Secrets().List(ctx, gitoperationslib.UserCI(), gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "TOKEN" {
		t.Errorf("on 17: %+v, %v", page, err)
	}
	if !slices.Equal(requests, []string{"/api/v1/user/actions/secrets"}) {
		t.Errorf("requests = %v", requests)
	}
}

func TestListUsersBy(t *testing.T) {
	var query string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("is_2fa_enabled")
		io.WriteString(w, `[{"id":1,"login":"bob"}]`)
	})
	ctx := context.Background()
	with := gitoperationslib.UserFilter{TwoFactor: gitoperationslib.Ptr(false)}
	if _, err := c.ListUsersBy(ctx, with, gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "16.0.0") {
		t.Errorf("before 16: %v", err)
	}
	// No filter needs no version.
	if _, err := c.ListUsersBy(ctx, gitoperationslib.UserFilter{}, gitoperationslib.ListOptions{}); err != nil || query != "" {
		t.Errorf("no filter: %v, query %q", err, query)
	}
	c.version = Version{16, 0, 7}
	page, err := c.ListUsersBy(ctx, with, gitoperationslib.ListOptions{})
	if err != nil || query != "false" || len(page.Items) != 1 {
		t.Errorf("on 16: %+v, %v, query %q", page, err, query)
	}
}

func TestAddMemberInvitation(t *testing.T) {
	addStatus, memberStatus := http.StatusNoContent, http.StatusNotFound
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method)
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(addStatus)
			if addStatus == http.StatusConflict {
				io.WriteString(w, `{"message":"team invite already exists [team_id: 2, email: b@example.invalid, invited_user_id: 0]"}`)
			}
		default:
			w.WriteHeader(memberStatus)
			if memberStatus == http.StatusOK {
				io.WriteString(w, `{"login":"bob"}`)
			}
		}
	})
	ctx := context.Background()

	// Before 16 there is nothing to check.
	if err := c.Teams().AddMember(ctx, 2, "bob"); err != nil || !slices.Equal(requests, []string{"PUT"}) {
		t.Errorf("before 16: %v, requests %v", err, requests)
	}

	c.version = Version{17, 0, 1}
	if err := c.Teams().AddMember(ctx, 2, "bob"); !errors.Is(err, gitoperationslib.ErrInvitationPending) {
		t.Errorf("invited: %v", err)
	}
	addStatus = http.StatusConflict
	if err := c.Teams().AddMember(ctx, 2, "bob"); !errors.Is(err, gitoperationslib.ErrInvitationPending) {
		t.Errorf("invited again: %v", err)
	}
	addStatus, memberStatus = http.StatusNoContent, http.StatusOK
	if err := c.Teams().AddMember(ctx, 2, "bob"); err != nil {
		t.Errorf("added: %v", err)
	}
}

func TestPost15Fields(t *testing.T) {
	key := (&sshKeyJSON{}).toSSHKey()
	if !key.UpdatedAt.IsZero() || key.Verified {
		t.Errorf("zero key = %+v", key)
	}
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":5,"title":"k","key":"ssh-ed25519 AAA","updated_at":"2026-11-05T00:00:00Z","verified":true}`)
	})
	got, err := c.SSHKeys().Get(context.Background(), 5)
	if err != nil || got.UpdatedAt.IsZero() || !got.Verified {
		t.Errorf("key = %+v, %v", got, err)
	}
	org := (&orgJSON{Username: "acme"}).toOrg()
	if !org.CreatedAt.IsZero() {
		t.Errorf("org = %+v", org)
	}
}

func TestPullFiltersByVersion(t *testing.T) {
	var query url.Values
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		io.WriteString(w, `[]`)
	})
	ctx := context.Background()
	filter := gitoperationslib.ListPROptions{Base: "main", Head: "feature"}
	if _, err := c.PullRequests().List(ctx, "acme", "widget", filter); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "16.0.0") {
		t.Errorf("before 16: %v", err)
	}
	// No filter needs no version.
	if _, err := c.PullRequests().List(ctx, "acme", "widget", gitoperationslib.ListPROptions{}); err != nil {
		t.Errorf("no filter: %v", err)
	}
	c.version = Version{16, 0, 7}
	if _, err := c.PullRequests().List(ctx, "acme", "widget", filter); err != nil || query.Get("base") != "main" || query.Get("head") != "feature" {
		t.Errorf("on 16: %v, query %v", err, query)
	}
	if _, err := c.PullRequests().List(ctx, "acme", "widget", gitoperationslib.ListPROptions{Head: "fork:feature"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user:branch: %v", err)
	}
}

func TestIssueLocksByVersion(t *testing.T) {
	status := http.StatusNoContent
	var requests []string
	var reason string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		reason = body["reason"]
		w.WriteHeader(status)
	})
	ctx := context.Background()
	if err := c.LockIssue(ctx, "acme", "widget", 3, "why"); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "17.0.0") {
		t.Errorf("before 17: %v", err)
	}
	if err := c.UnlockIssue(ctx, "acme", "widget", 3); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("before 17: %v", err)
	}
	if len(requests) != 0 {
		t.Errorf("sent %v", requests)
	}
	c.version = Version{17, 0, 1}
	if err := c.LockIssue(ctx, "acme", "widget", 3, ""); err == nil || len(requests) != 0 {
		t.Errorf("no reason: %v, sent %v", err, requests)
	}
	if err := c.LockIssue(ctx, "acme", "widget", 3, gitoperationslib.LockReasonSpam); err != nil || reason != "spam" {
		t.Errorf("Lock: %v, reason %q", err, reason)
	}
	if err := c.UnlockIssue(ctx, "acme", "widget", 3); err != nil {
		t.Errorf("Unlock: %v", err)
	}
	if want := []string{"PUT /api/v1/repos/acme/widget/issues/3/lock", "DELETE /api/v1/repos/acme/widget/issues/3/lock"}; !slices.Equal(requests, want) {
		t.Errorf("requests = %v", requests)
	}
	// Already in that state.
	status = http.StatusConflict
	if err := c.LockIssue(ctx, "acme", "widget", 3, "why"); err != nil {
		t.Errorf("Lock twice: %v", err)
	}
	if err := c.UnlockIssue(ctx, "acme", "widget", 3); err != nil {
		t.Errorf("Unlock twice: %v", err)
	}
	status = http.StatusNotFound
	if err := c.LockIssue(ctx, "acme", "widget", 3, "why"); err == nil {
		t.Error("a 404 was swallowed")
	}
}

func TestExtraLinesByVersion(t *testing.T) {
	var sent map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&sent)
			io.WriteString(w, `{"id":4}`)
			return
		}
		io.WriteString(w, `[{"id":9,"path":"a.go","position":2,"extra_lines_count":3}]`)
	})
	ctx := context.Background()
	opts := gitoperationslib.CreateReviewCommentOptions{Path: "a.go", Body: "b", Line: 2, ExtraLines: 3}
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 1, opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("before 16: %v", err)
	}
	c.version = Version{16, 0, 7}
	got, err := c.CreateReviewComment(ctx, "acme", "widget", 1, opts)
	if err != nil || got.ExtraLines != 3 {
		t.Fatalf("on 16: %+v, %v", got, err)
	}
	if comment := sent["comments"].([]any)[0].(map[string]any); comment["extra_lines_count"] != float64(3) {
		t.Errorf("comment = %v", comment)
	}
	opts.ExtraLines = -1
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 1, opts); err == nil {
		t.Error("negative ExtraLines accepted")
	}
}

func TestCompareAsksForTheCountOnly(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("files") != "false" || q.Get("verification") != "false" {
			t.Errorf("query = %v", q)
		}
		io.WriteString(w, `{"total_commits":2}`)
	})
	got, err := c.Branches().Compare(context.Background(), "acme", "widget", "main", "dev")
	if err != nil || got.Ahead != 2 || got.Behind != 2 {
		t.Errorf("Compare = %+v, %v", got, err)
	}
}

func TestTokensOf(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			io.WriteString(w, `{"id":4,"name":"ci","sha1":"abc"}`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			io.WriteString(w, `[{"id":4,"name":"ci"}]`)
		}
	})
	ctx := context.Background()
	if _, err := c.TokensOf("bob").List(ctx, gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("before 16: %v", err)
	}
	c.version = Version{16, 0, 7}
	if _, err := c.TokensOf("").List(ctx, gitoperationslib.ListOptions{}); err == nil {
		t.Error("no login accepted")
	}
	if len(requests) != 0 {
		t.Errorf("sent %v", requests)
	}
	tokens := c.TokensOf("bob")
	tok, err := tokens.Create(ctx, gitoperationslib.CreateTokenOptions{Name: "ci", Scopes: []string{"read:user"}})
	if err != nil || tok.Secret != "abc" {
		t.Errorf("Create = %+v, %v", tok, err)
	}
	if err := tokens.DeleteByName(ctx, "ci"); err != nil {
		t.Error(err)
	}
	want := []string{"POST /api/v1/admin/users/bob/tokens", "GET /api/v1/admin/users/bob/tokens", "DELETE /api/v1/admin/users/bob/tokens/4"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %v", requests)
	}
}
