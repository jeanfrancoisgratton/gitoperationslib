package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

type pullWrite struct {
	request string
	body    map[string]any
}

// pullServer serves pull request 7 in the given state, with bob as reviewer.
// Pull request 8 does not exist, and 9 is forbidden.
func pullServer(t *testing.T, state *string) (*Client, *[]pullWrite) {
	t.Helper()
	var writes []pullWrite
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/2.0/repositories/acme/widget/pullrequests")
		if r.Method != http.MethodGet {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, pullWrite{r.Method + " " + path, body})
		}
		switch {
		case path == "/8":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"not found"}}`)
		case path == "/9":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":{"message":"forbidden"}}`)
		case strings.HasSuffix(path, "/comments"):
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":55}`)
		case strings.HasSuffix(path, "/merge"):
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{}`)
		default:
			fmt.Fprintf(w, `{"id":7,"title":"T","state":%q,"reviewers":[{"uuid":"{bob}"}],"source":{"branch":{"name":"topic"}},"destination":{"branch":{"name":"main"}}}`, *state)
		}
	})
	return c, &writes
}

func requestsOf(writes []pullWrite) []string {
	out := []string{}
	for _, w := range writes {
		out = append(out, w.request)
	}
	return out
}

func TestCreatePullRequestBitbucket(t *testing.T) {
	state := "OPEN"
	c, writes := pullServer(t, &state)
	ctx := context.Background()
	pr, err := c.PullRequests().Create(ctx, "acme", "widget", gitoperationslib.CreatePROptions{Title: "T", Body: "B", Head: "topic", Base: "main", Draft: true})
	if err != nil || pr.Number != 7 || pr.Head.Ref != "topic" || pr.Base.Ref != "main" {
		t.Fatalf("Create = %+v, %v", pr, err)
	}
	got, _ := json.Marshal((*writes)[0].body)
	want := `{"description":"B","destination":{"branch":{"name":"main"}},"draft":true,"source":{"branch":{"name":"topic"}},"title":"T"}`
	if string(got) != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}

	*writes = nil
	for name, opts := range map[string]gitoperationslib.CreatePROptions{
		"no title": {Head: "topic", Base: "main"},
		"no head":  {Title: "T", Base: "main"},
		"no base":  {Title: "T", Head: "topic"},
	} {
		if _, err := c.PullRequests().Create(ctx, "acme", "widget", opts); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
	for name, opts := range map[string]gitoperationslib.CreatePROptions{
		"fork":             {Title: "T", Head: "alice:topic", Base: "main"},
		"maintainer edits": {Title: "T", Head: "topic", Base: "main", AllowMaintainerEdit: gitoperationslib.Ptr(true)},
	} {
		if _, err := c.PullRequests().Create(ctx, "acme", "widget", opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", requestsOf(*writes))
	}
}

func TestEditPullRequestBitbucket(t *testing.T) {
	state := "OPEN"
	c, writes := pullServer(t, &state)
	ctx := context.Background()
	prs := c.PullRequests()
	closed, open, bad := gitoperationslib.PRStateClosed, gitoperationslib.PRStateOpen, gitoperationslib.PRState("bad")

	// The fields go first, and closing declines afterwards.
	_, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{
		Title: gitoperationslib.Ptr("New"), Body: gitoperationslib.Ptr(""), Base: gitoperationslib.Ptr("dev"), Draft: gitoperationslib.Ptr(false), State: &closed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := requestsOf(*writes); !slices.Equal(got, []string{"PUT /7", "POST /7/decline"}) {
		t.Errorf("requests = %v", got)
	}
	got, _ := json.Marshal((*writes)[0].body)
	if want := `{"description":"","destination":{"branch":{"name":"dev"}},"draft":false,"title":"New"}`; string(got) != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}

	// Closing alone sends no PUT.
	*writes = nil
	if _, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{State: &closed}); err != nil {
		t.Fatal(err)
	}
	if got := requestsOf(*writes); !slices.Equal(got, []string{"POST /7/decline"}) {
		t.Errorf("requests = %v", got)
	}

	// "Open" on an open pull request changes nothing.
	*writes = nil
	if _, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{State: &open}); err != nil {
		t.Error(err)
	}

	// Refused before anything is written, even with other fields set.
	title := gitoperationslib.Ptr("New")
	if _, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{Title: title, State: &bad}); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("invalid state: err = %v", err)
	}
	if _, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{AllowMaintainerEdit: gitoperationslib.Ptr(true)}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("maintainer edits: err = %v", err)
	}
	state = "DECLINED"
	if _, err := prs.Edit(ctx, "acme", "widget", 7, gitoperationslib.EditPROptions{Title: title, State: &open}); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "declined") {
		t.Errorf("reopening: err = %v, want ErrNotSupported naming the state", err)
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", requestsOf(*writes))
	}
}

func reviewerUUIDs(t *testing.T, w pullWrite) []string {
	t.Helper()
	raw, ok := w.body["reviewers"].([]any)
	if !ok {
		t.Fatalf("no reviewers in %v", w.body)
	}
	out := []string{}
	for _, r := range raw {
		out = append(out, r.(map[string]any)["uuid"].(string))
	}
	sort.Strings(out)
	return out
}

func TestReviewersBitbucket(t *testing.T) {
	state := "OPEN"
	c, writes := pullServer(t, &state)
	ctx := context.Background()
	prs := c.PullRequests()

	// bob is already a reviewer: alice is added to him, and the title goes along.
	if err := prs.RequestReviewers(ctx, "acme", "widget", 7, []string{"alice"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reviewerUUIDs(t, (*writes)[0]); !slices.Equal(got, []string{"{alice}", "{bob}"}) {
		t.Errorf("reviewers = %v", got)
	}
	if (*writes)[0].body["title"] != "T" {
		t.Errorf("body = %v, want the title kept", (*writes)[0].body)
	}

	*writes = nil
	if err := prs.RemoveReviewers(ctx, "acme", "widget", 7, []string{"bob"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reviewerUUIDs(t, (*writes)[0]); len(got) != 0 {
		t.Errorf("reviewers = %v, want none", got)
	}

	*writes = nil
	if err := prs.RequestReviewers(ctx, "acme", "widget", 7, nil, []string{"devs"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("team reviewers: err = %v", err)
	}
	if err := prs.RemoveReviewers(ctx, "acme", "widget", 7, nil, []string{"devs"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("removing team reviewers: err = %v", err)
	}
	if err := prs.RequestReviewers(ctx, "acme", "widget", 7, []string{"nobody"}, nil); err == nil {
		t.Error("unknown user accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", requestsOf(*writes))
	}
}

func TestIsMergedBitbucket(t *testing.T) {
	state := "MERGED"
	c, _ := pullServer(t, &state)
	ctx := context.Background()
	prs := c.PullRequests()
	if ok, err := prs.IsMerged(ctx, "acme", "widget", 7); !ok || err != nil {
		t.Errorf("merged: %v, %v", ok, err)
	}
	state = "OPEN"
	if ok, err := prs.IsMerged(ctx, "acme", "widget", 7); ok || err != nil {
		t.Errorf("open: %v, %v", ok, err)
	}
	if ok, err := prs.IsMerged(ctx, "acme", "widget", 8); ok || err != nil {
		t.Errorf("no such pull request: %v, %v", ok, err)
	}
	if _, err := prs.IsMerged(ctx, "acme", "widget", 9); !gitoperationslib.IsForbidden(err) {
		t.Errorf("forbidden: err = %v, want 403", err)
	}
	if err := prs.CancelAutoMerge(ctx, "acme", "widget", 7); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("CancelAutoMerge: err = %v", err)
	}
}

// A 202 means the merge runs in the background: Merge waits for its result.
func TestMergeAsyncBitbucket(t *testing.T) {
	state := "MERGED"
	c, _ := pullServer(t, &state)
	prs := c.PullRequests()
	if err := prs.Merge(context.Background(), "acme", "widget", 7, gitoperationslib.MergeOptions{}); err != nil {
		t.Errorf("already merged at the first check: %v", err)
	}

	state = "OPEN"
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := prs.Merge(ctx, "acme", "widget", 7, gitoperationslib.MergeOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled while waiting: err = %v, want the context's error", err)
	}
}

func TestCreateReviewBitbucket(t *testing.T) {
	state := "OPEN"
	c, writes := pullServer(t, &state)
	ctx := context.Background()
	prs := c.PullRequests()

	for _, tc := range []struct {
		name  string
		opts  gitoperationslib.CreateReviewOptions
		state gitoperationslib.ReviewState
		id    int64
		want  []string
	}{
		{"approve", gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove},
			gitoperationslib.ReviewApproved, 0, []string{"POST /7/approve"}},
		{"approve with a comment", gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove, Body: "LGTM"},
			gitoperationslib.ReviewApproved, 55, []string{"POST /7/approve", "POST /7/comments"}},
		{"request changes", gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventRequestChanges},
			gitoperationslib.ReviewChangesRequested, 0, []string{"POST /7/request-changes"}},
		{"comment", gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventComment, Body: "Hm"},
			gitoperationslib.ReviewCommented, 55, []string{"POST /7/comments"}},
	} {
		*writes = nil
		got, err := prs.CreateReview(ctx, "acme", "widget", 7, tc.opts)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got.State != tc.state || got.ID != tc.id || got.Reviewer != "alice" || got.Body != tc.opts.Body || got.SubmittedAt.IsZero() {
			t.Errorf("%s: review = %+v", tc.name, got)
		}
		if requests := requestsOf(*writes); !slices.Equal(requests, tc.want) {
			t.Errorf("%s: requests = %v, want %v", tc.name, requests, tc.want)
		}
	}

	*writes = nil
	if _, err := prs.CreateReview(ctx, "acme", "widget", 7, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint((*writes)[1].body); got != "map[content:map[raw:x]]" {
		t.Errorf("comment body = %s", got)
	}

	*writes = nil
	if _, err := prs.CreateReview(ctx, "acme", "widget", 7, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventComment}); err == nil {
		t.Error("comment without a body accepted")
	}
	if _, err := prs.CreateReview(ctx, "acme", "widget", 7, gitoperationslib.CreateReviewOptions{Event: "BAD"}); err == nil {
		t.Error("invalid event accepted")
	}
	if _, err := prs.CreateReview(ctx, "acme", "widget", 7, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove, CommitID: "abc"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("reviewing a commit: err = %v", err)
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", requestsOf(*writes))
	}
}
