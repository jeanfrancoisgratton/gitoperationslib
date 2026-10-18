package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const pullResponse = `{
	"number": 3, "title": "Feature", "body": "b", "state": "open", "draft": true, "user": {"login": "alice"},
	"head": {"ref": "feature", "sha": "abc", "repo": {"full_name": "acme/widget"}},
	"base": {"ref": "main", "sha": "def", "repo": {"full_name": "acme/widget"}},
	"mergeable": null, "merged": false, "html_url": "https://github.com/acme/widget/pull/3",
	"created_at": "2026-10-17T10:00:00Z", "updated_at": "2026-10-17T11:00:00Z"
}`

func TestPullConversion(t *testing.T) {
	var j gh.PullRequest
	if err := json.Unmarshal([]byte(pullResponse), &j); err != nil {
		t.Fatal(err)
	}
	pr := toPR(&j)
	if pr.Number != 3 || !pr.Draft || pr.Author != "alice" || pr.Head.Repo != "acme/widget" || pr.Head.SHA != "abc" ||
		pr.Base.Ref != "main" || pr.Mergeable != nil || pr.Merged || !pr.MergedAt.IsZero() {
		t.Errorf("got %+v (Mergeable must stay nil while GitHub computes it)", pr)
	}
	for state, want := range map[string]gitoperationslib.ReviewState{
		"APPROVED": gitoperationslib.ReviewApproved, "CHANGES_REQUESTED": gitoperationslib.ReviewChangesRequested,
		"COMMENTED": gitoperationslib.ReviewCommented, "DISMISSED": gitoperationslib.ReviewDismissed, "PENDING": gitoperationslib.ReviewPending,
	} {
		if got := toReview(&gh.PullRequestReview{State: gh.Ptr(state)}).State; got != want {
			t.Errorf("%s -> %q, want %q", state, got, want)
		}
	}
}

func TestPullsRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls/3/merge" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls/3/merge":
			io.WriteString(w, `{"merged":true,"sha":"m"}`)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls" && r.Method == http.MethodGet:
			io.WriteString(w, "["+pullResponse+"]")
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls/3/reviews" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":1,"user":{"login":"bob"},"state":"APPROVED"}]`)
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls/3/reviews":
			io.WriteString(w, `{"id":2,"user":{"login":"bob"},"state":"COMMENTED"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/repos/acme/widget/pulls":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, pullResponse)
		default:
			io.WriteString(w, pullResponse)
		}
	})
	ctx := context.Background()
	prs := c.PullRequests()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := prs.List(ctx, "acme", "widget", gitoperationslib.ListPROptions{State: gitoperationslib.PRStateClosed})
	check(err)
	_, err = prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{
		Title: "Feature", Head: "feature", Base: "main", Draft: true, AllowMaintainerEdit: gitoperationslib.Ptr(true),
	})
	check(err)
	open := gitoperationslib.PRStateOpen
	_, err = prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Base: gitoperationslib.Ptr("dev"), State: &open})
	check(err)
	// DeleteBranch: look up the head, merge, delete the ref.
	check(prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{
		Method: gitoperationslib.MergeMethodSquash, Title: "t", Message: "m", HeadSHA: "abc", DeleteBranch: true,
	}))
	merged, err := prs.IsMerged(ctx, "acme", "widget", 3)
	check(err)
	if !merged {
		t.Error("IsMerged = false on 204")
	}
	check(prs.RequestReviewers(ctx, "acme", "widget", 3, []string{"bob"}, nil))
	_, err = prs.ListReviews(ctx, "acme", "widget", 3, gitoperationslib.ListOptions{})
	check(err)
	_, err = prs.CreateReview(ctx, "acme", "widget", 3, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove})
	check(err)

	wantRequests := []string{
		"GET /api/v3/repos/acme/widget/pulls?state=closed",
		"POST /api/v3/repos/acme/widget/pulls?",
		"PATCH /api/v3/repos/acme/widget/pulls/3?",
		"GET /api/v3/repos/acme/widget/pulls/3?",
		"PUT /api/v3/repos/acme/widget/pulls/3/merge?",
		"DELETE /api/v3/repos/acme/widget/git/refs/heads/feature?",
		"GET /api/v3/repos/acme/widget/pulls/3/merge?",
		"POST /api/v3/repos/acme/widget/pulls/3/requested_reviewers?",
		"GET /api/v3/repos/acme/widget/pulls/3/reviews?",
		"POST /api/v3/repos/acme/widget/pulls/3/reviews?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		1: {"title": "Feature", "head": "feature", "base": "main", "draft": true, "maintainer_can_modify": true},
		2: {"base": "dev", "state": "open"}, // go-github sends the base branch as a plain name
		4: {"commit_title": "t", "commit_message": "m", "sha": "abc", "merge_method": "squash"},
		7: {"reviewers": []any{"bob"}},
		9: {"event": "APPROVE"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestMergeDeleteBranchRefusesFork(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		io.WriteString(w, `{"number":3,"head":{"ref":"feature","repo":{"full_name":"someone/widget"}}}`)
	})
	err := c.PullRequests().Merge(context.Background(), "acme", "widget", 3, gitoperationslib.MergeOptions{DeleteBranch: true})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("err = %v, want ErrNotSupported", err)
	}
}

func TestPullsRejected(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	prs := c.PullRequests()
	for _, m := range []gitoperationslib.MergeMethod{gitoperationslib.MergeMethodRebaseMerge, gitoperationslib.MergeMethodFastForwardOnly, gitoperationslib.MergeMethodManuallyMerged} {
		if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{Method: m}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v", m, err)
		}
	}
	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{WhenChecksSucceed: true, DeleteBranch: true}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("auto-merge with branch deletion: err = %v", err)
	}
	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{Method: "octopus"}); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("invalid method: err = %v, want a validation error", err)
	}
	if _, err := prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Head: "f", Base: "main"}); err == nil {
		t.Error("missing title accepted")
	}
}

// graphqlServer answers REST pull request lookups with draft state *draft and
// records GraphQL requests; respond gives the GraphQL response body.
func graphqlServer(t *testing.T, draft *bool, respond func(query string) string) (*Client, *[]map[string]any) {
	t.Helper()
	var calls []map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/graphql":
			if r.Header.Get("Authorization") != "Bearer pat" {
				t.Error("GraphQL request without the token")
			}
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			calls = append(calls, req)
			body := respond(req["query"].(string))
			if strings.Contains(req["query"].(string), "markPullRequestReadyForReview") {
				*draft = false
			} else if strings.Contains(req["query"].(string), "convertPullRequestToDraft") {
				*draft = true
			}
			io.WriteString(w, body)
		case r.URL.Path == "/api/v3/repos/acme/widget/pulls/3":
			fmt.Fprintf(w, `{"number":3,"node_id":"PR_node3","draft":%v,"title":"t","state":"open"}`, *draft)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	return c, &calls
}

func TestEditDraftViaGraphQL(t *testing.T) {
	draft := true
	c, calls := graphqlServer(t, &draft, func(string) string { return `{"data":{"x":{"clientMutationId":null}}}` })
	ctx := context.Background()
	prs := c.PullRequests()

	pr, err := prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(false)})
	if err != nil || pr.Draft {
		t.Fatalf("ready = %+v, %v", pr, err)
	}
	pr, err = prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(true)})
	if err != nil || !pr.Draft {
		t.Fatalf("to draft = %+v, %v", pr, err)
	}
	// Already a draft: no mutation is sent.
	if _, err := prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("GraphQL calls = %d, want 2", len(*calls))
	}
	for i, want := range []string{"markPullRequestReadyForReview", "convertPullRequestToDraft"} {
		q := (*calls)[i]["query"].(string)
		vars := (*calls)[i]["variables"].(map[string]any)
		if !strings.Contains(q, want) || vars["id"] != "PR_node3" {
			t.Errorf("call %d = %v", i, (*calls)[i])
		}
	}
}

func TestAutoMergeViaGraphQL(t *testing.T) {
	draft := false
	c, calls := graphqlServer(t, &draft, func(string) string { return `{"data":{"x":{"clientMutationId":null}}}` })
	ctx := context.Background()
	prs := c.PullRequests()

	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{
		Method: gitoperationslib.MergeMethodSquash, Title: "T", Message: "M", HeadSHA: "abc", WhenChecksSucceed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := prs.CancelAutoMerge(ctx, "acme", "widget", 3); err != nil {
		t.Fatal(err)
	}
	enable, disable := (*calls)[0], (*calls)[1]
	if !strings.Contains(enable["query"].(string), "enablePullRequestAutoMerge") ||
		fmt.Sprint(enable["variables"]) != "map[body:M id:PR_node3 method:SQUASH sha:abc title:T]" {
		t.Errorf("enable = %v", enable)
	}
	if !strings.Contains(disable["query"].(string), "disablePullRequestAutoMerge") || disable["variables"].(map[string]any)["id"] != "PR_node3" {
		t.Errorf("disable = %v", disable)
	}
}

func TestGraphQLErrors(t *testing.T) {
	draft := false
	for _, tt := range []struct {
		body   string
		status int
		msg    string
	}{
		{`{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`, 404, "Could not resolve to a node"},
		{`{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"},{"message":"second"}]}`, 403, "Resource not accessible"},
		{`{"errors":[{"message":"Pull request is in clean status"}]}`, 422, "Pull request is in clean status"},
	} {
		c, _ := graphqlServer(t, &draft, func(string) string { return tt.body })
		err := c.PullRequests().Merge(context.Background(), "acme", "widget", 3, gitoperationslib.MergeOptions{WhenChecksSucceed: true})
		var apiErr *gitoperationslib.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.status || apiErr.Message != tt.msg {
			t.Errorf("%s: err = %v", tt.body, err)
		}
	}
}

func TestGraphQLURL(t *testing.T) {
	for base, want := range map[string]string{
		"":                                "https://api.github.com/graphql",
		"https://ghe.example.com":         "https://ghe.example.com/api/graphql",
		"https://ghe.example.com/api/v3/": "https://ghe.example.com/api/graphql",
	} {
		var opts []Option
		if base != "" {
			opts = append(opts, WithEnterpriseURL(base, ""))
		}
		c, err := New("", opts...)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.graphqlURL(); got != want {
			t.Errorf("base %q: graphqlURL = %q, want %q", base, got, want)
		}
	}
}
