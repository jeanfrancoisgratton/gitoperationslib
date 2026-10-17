package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const pullResponse = `{
	"number": 3, "title": "Feature", "body": "b", "state": "open", "draft": false, "user": {"login": "alice"},
	"head": {"ref": "feature", "sha": "abc", "repo": {"full_name": "acme/widget"}},
	"base": {"ref": "main", "sha": "def", "repo": {"full_name": "acme/widget"}},
	"mergeable": true, "merged": false, "merged_by": null, "merged_at": null, "merge_commit_sha": "",
	"html_url": "https://h/acme/widget/pulls/3", "created_at": "2026-10-17T10:00:00Z",
	"updated_at": "2026-10-17T11:00:00Z", "closed_at": null
}`

func TestPullConversion(t *testing.T) {
	var j pullJSON
	if err := json.Unmarshal([]byte(pullResponse), &j); err != nil {
		t.Fatal(err)
	}
	pr := j.toPR()
	if pr.Number != 3 || pr.Title != "Feature" || pr.State != gitoperationslib.PRStateOpen || pr.Author != "alice" ||
		pr.Head != (gitoperationslib.PRBranch{Ref: "feature", SHA: "abc", Repo: "acme/widget"}) || pr.Base.Ref != "main" ||
		pr.Mergeable == nil || !*pr.Mergeable || pr.Merged || pr.MergedBy != "" || !pr.MergedAt.IsZero() || !pr.ClosedAt.IsZero() {
		t.Errorf("got %+v", pr)
	}
}

func TestDraftTitle(t *testing.T) {
	for _, tt := range []struct {
		title string
		draft bool
		want  string
	}{
		{"Feature", true, "WIP: Feature"},
		{"WIP: Feature", true, "WIP: Feature"},
		{"[wip] Feature", true, "[wip] Feature"},
		{"WIP: Feature", false, "Feature"},
		{"[WIP] Feature", false, "Feature"},
		{"wip:Feature", false, "Feature"},
		{"Feature", false, "Feature"},
		{"WIPE the floor", true, "WIP: WIPE the floor"},
	} {
		if got := draftTitle(tt.title, tt.draft); got != tt.want {
			t.Errorf("draftTitle(%q, %v) = %q, want %q", tt.title, tt.draft, got, tt.want)
		}
	}
}

func TestReviewConversion(t *testing.T) {
	for state, want := range map[string]gitoperationslib.ReviewState{
		"APPROVED": gitoperationslib.ReviewApproved, "REQUEST_CHANGES": gitoperationslib.ReviewChangesRequested,
		"COMMENT": gitoperationslib.ReviewCommented, "PENDING": gitoperationslib.ReviewPending,
		"REQUEST_REVIEW": gitoperationslib.ReviewRequested, "SOMETHING_NEW": "something_new",
	} {
		if got := (&reviewJSON{State: state}).toReview().State; got != want {
			t.Errorf("%s -> %q, want %q", state, got, want)
		}
	}
	if got := (&reviewJSON{State: "APPROVED", Dismissed: true}).toReview().State; got != gitoperationslib.ReviewDismissed {
		t.Errorf("dismissed approval -> %q", got)
	}
}

func TestPullsRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.URL.Path == "/api/v1/repos/acme/widget/pulls/3/merge" && r.Method == http.MethodGet:
			http.NotFound(w, r) // not merged
		case r.URL.Path == "/api/v1/repos/acme/widget/pulls/3/merge", r.URL.Path == "/api/v1/repos/acme/widget/pulls/3/requested_reviewers":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/v1/repos/acme/widget/pulls" && r.Method == http.MethodGet:
			io.WriteString(w, "["+pullResponse+"]")
		case r.URL.Path == "/api/v1/repos/acme/widget/pulls/3/reviews" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":1,"user":{"login":"bob"},"state":"APPROVED"}]`)
		case r.URL.Path == "/api/v1/repos/acme/widget/pulls/3/reviews":
			io.WriteString(w, `{"id":2,"user":{"login":"bob"},"state":"COMMENT","body":"hi"}`)
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

	_, err := prs.List(ctx, "acme", "widget", gitoperationslib.ListPROptions{State: gitoperationslib.PRStateAll, ListOptions: gitoperationslib.ListOptions{PerPage: 5}})
	check(err)
	_, err = prs.Get(ctx, "acme", "widget", 3)
	check(err)
	_, err = prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Title: "Feature", Head: "feature", Base: "main"})
	check(err)
	closed := gitoperationslib.PRStateClosed
	_, err = prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Body: gitoperationslib.Ptr(""), State: &closed})
	check(err)
	check(prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{
		Method: gitoperationslib.MergeMethodFastForwardOnly, Title: "t", HeadSHA: "abc", DeleteBranch: true,
	}))
	check(prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{}))
	merged, err := prs.IsMerged(ctx, "acme", "widget", 3)
	check(err)
	if merged {
		t.Error("IsMerged = true on 404")
	}
	check(prs.RequestReviewers(ctx, "acme", "widget", 3, []string{"bob"}, []string{"devs"}))
	check(prs.RemoveReviewers(ctx, "acme", "widget", 3, []string{"bob"}, nil))
	reviews, err := prs.ListReviews(ctx, "acme", "widget", 3, gitoperationslib.ListOptions{})
	check(err)
	if len(reviews.Items) != 1 || reviews.Items[0].State != gitoperationslib.ReviewApproved || reviews.Items[0].Reviewer != "bob" {
		t.Errorf("ListReviews = %+v", reviews)
	}
	review, err := prs.CreateReview(ctx, "acme", "widget", 3, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventComment, Body: "hi"})
	check(err)
	if review.State != gitoperationslib.ReviewCommented {
		t.Errorf("CreateReview = %+v", review)
	}

	wantRequests := []string{
		"GET /api/v1/repos/acme/widget/pulls?limit=5&state=all",
		"GET /api/v1/repos/acme/widget/pulls/3?",
		"POST /api/v1/repos/acme/widget/pulls?",
		"PATCH /api/v1/repos/acme/widget/pulls/3?",
		"POST /api/v1/repos/acme/widget/pulls/3/merge?",
		"POST /api/v1/repos/acme/widget/pulls/3/merge?",
		"GET /api/v1/repos/acme/widget/pulls/3/merge?",
		"POST /api/v1/repos/acme/widget/pulls/3/requested_reviewers?",
		"DELETE /api/v1/repos/acme/widget/pulls/3/requested_reviewers?",
		"GET /api/v1/repos/acme/widget/pulls/3/reviews?",
		"POST /api/v1/repos/acme/widget/pulls/3/reviews?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		2:  {"title": "Feature", "head": "feature", "base": "main"},
		3:  {"body": "", "state": "closed"},
		4:  {"Do": "fast-forward-only", "MergeTitleField": "t", "head_commit_id": "abc", "delete_branch_after_merge": true},
		5:  {"Do": "merge"},
		7:  {"reviewers": []any{"bob"}, "team_reviewers": []any{"devs"}},
		8:  {"reviewers": []any{"bob"}},
		10: {"event": "COMMENT", "body": "hi"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestPullDrafts(t *testing.T) {
	var titles []string
	honour := true
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title string `json:"title"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		title := body.Title
		if r.Method == http.MethodGet {
			title = "WIP: Feature"
		} else {
			titles = append(titles, r.Method+" "+title)
		}
		_, isDraft := wipPrefix(title)
		fmt.Fprintf(w, `{"number":3,"title":%q,"draft":%v,"state":"open"}`, title, isDraft && honour)
	})
	ctx := context.Background()
	prs := c.PullRequests()

	pr, err := prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Title: "Feature", Head: "f", Base: "main", Draft: true})
	if err != nil || !pr.Draft || pr.Title != "WIP: Feature" {
		t.Errorf("create draft = %+v, %v", pr, err)
	}
	// No new title: the current one is fetched and its prefix removed.
	pr, err = prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(false)})
	if err != nil || pr.Draft || pr.Title != "Feature" {
		t.Errorf("mark ready = %+v, %v", pr, err)
	}
	pr, err = prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{Title: gitoperationslib.Ptr("Renamed"), Draft: gitoperationslib.Ptr(true)})
	if err != nil || pr.Title != "WIP: Renamed" {
		t.Errorf("rename as draft = %+v, %v", pr, err)
	}
	if fmt.Sprint(titles) != "[POST WIP: Feature PATCH Feature PATCH WIP: Renamed]" {
		t.Errorf("titles sent = %v", titles)
	}

	// A server with other prefixes does not honour ours: report it.
	honour = false
	pr, err = prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Title: "Feature", Head: "f", Base: "main", Draft: true})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) || pr == nil || pr.Number != 3 {
		t.Errorf("unhonoured draft = %+v, %v; want PR and ErrNotSupported", pr, err)
	}
}

func TestPullsRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	prs := c.PullRequests()
	if _, err := prs.Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Title: "x", Head: "f"}); err == nil {
		t.Error("missing base accepted")
	}
	all := gitoperationslib.PRStateAll
	if _, err := prs.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditPROptions{State: &all}); err == nil {
		t.Error("state all accepted on edit")
	}
	if err := prs.Merge(ctx, "acme", "widget", 3, gitoperationslib.MergeOptions{Method: "octopus"}); err == nil {
		t.Error("invalid merge method accepted")
	}
	if _, err := prs.CreateReview(ctx, "acme", "widget", 3, gitoperationslib.CreateReviewOptions{Event: "lgtm"}); err == nil {
		t.Error("invalid review event accepted")
	}
}
