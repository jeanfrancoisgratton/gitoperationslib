package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestPullFiltersAddTheOwner(t *testing.T) {
	var head, base string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		head, base = r.URL.Query().Get("head"), r.URL.Query().Get("base")
		io.WriteString(w, `[]`)
	})
	for _, tc := range []struct{ in, want string }{{"feature", "acme:feature"}, {"fork:feature", "fork:feature"}, {"", ""}} {
		if _, err := c.PullRequests().List(context.Background(), "acme", "widget", gitoperationslib.ListPROptions{Head: tc.in, Base: "main"}); err != nil {
			t.Fatal(err)
		}
		if head != tc.want || base != "main" {
			t.Errorf("Head %q: head = %q, base = %q", tc.in, head, base)
		}
	}
}

func TestReviewCommentOnSeveralLines(t *testing.T) {
	var sent map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sent)
		fmt.Fprint(w, `{"id":3,"path":"a.go","side":"RIGHT","start_line":2,"line":4,"original_start_line":2,"original_line":4}`)
	})
	got, err := c.CreateReviewComment(context.Background(), "acme", "widget", 1, gitoperationslib.CreateReviewCommentOptions{Path: "a.go", Body: "b", Line: 2, ExtraLines: 2, CommitID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if sent["start_line"] != float64(2) || sent["line"] != float64(4) || sent["start_side"] != "RIGHT" {
		t.Errorf("sent %v", sent)
	}
	if got.Line != 2 || got.ExtraLines != 2 {
		t.Errorf("comment = %+v", got)
	}
}

func TestIssueLocks(t *testing.T) {
	var requests []string
	var reason any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		reason = body["lock_reason"]
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	if err := c.LockIssue(ctx, "acme", "widget", 3, gitoperationslib.LockReasonTooHeated); err != nil || reason != "too heated" {
		t.Errorf("Lock: %v, reason %v", err, reason)
	}
	if err := c.LockIssue(ctx, "acme", "widget", 3, ""); err != nil || reason != nil {
		t.Errorf("Lock without a reason: %v, reason %v", err, reason)
	}
	if err := c.UnlockIssue(ctx, "acme", "widget", 3); err != nil {
		t.Error(err)
	}
	if len(requests) != 3 || requests[2] != "DELETE /api/v3/repos/acme/widget/issues/3/lock" {
		t.Errorf("requests = %v", requests)
	}
}

func TestMissingLabelNotCreated(t *testing.T) {
	var writes []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method+" "+r.URL.Path)
		}
		fmt.Fprint(w, `[{"id":1,"name":"Bug"}]`)
	})
	ctx := context.Background()
	if err := c.Issues().AddLabels(ctx, "acme", "widget", 1, []string{"bug", "typo"}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("AddLabels: %v", err)
	}
	if _, err := c.Issues().Create(ctx, "acme", "widget", gitoperationslib.CreateIssueOptions{Title: "t", Labels: []string{"typo"}}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Create: %v", err)
	}
	if len(writes) != 0 {
		t.Errorf("wrote %v", writes)
	}
	// GitHub's label names ignore case.
	if err := c.Issues().AddLabels(ctx, "acme", "widget", 1, []string{"BUG"}); err != nil || len(writes) != 1 {
		t.Errorf("AddLabels(BUG): %v, %v", err, writes)
	}
}

func TestClearDueOnGitHub(t *testing.T) {
	var sent string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		sent = strings.TrimSpace(string(data))
		fmt.Fprint(w, `{"number":3,"title":"v","state":"open"}`)
	})
	got, err := c.Milestones().Edit(context.Background(), "acme", "widget", 3, gitoperationslib.EditMilestoneOptions{ClearDueOn: true})
	if err != nil || !got.DueOn.IsZero() || sent != `{"due_on":null}` {
		t.Errorf("Edit = %+v, %v, sent %s", got, err, sent)
	}
}

func TestDeletedIssueIsNotFound(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, `{"message":"This issue was deleted"}`)
	})
	if _, err := c.Issues().Get(context.Background(), "acme", "widget", 1); !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
}
