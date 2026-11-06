package webhook

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestIssues(t *testing.T) {
	ev := parseFixture(t, "forgejo-issues")
	if ev.Name != "issues" || ev.Action != "opened" || ev.Sender != "gitopslib-admin" {
		t.Errorf("event = %+v", ev)
	}
	got, err := ev.Issues()
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "opened" || got.Issue.Number != 1 || got.Issue.Title != "Issue one" || got.Issue.Body != "text" ||
		got.Issue.State != gitoperationslib.IssueStateOpen || got.Issue.Author != "gitopslib-admin" || got.Issue.CreatedAt.IsZero() {
		t.Errorf("issue = %+v", got)
	}
	if _, err := ev.IssueComment(); err == nil {
		t.Error("IssueComment() on an issues delivery succeeded")
	}
}

func TestIssueComment(t *testing.T) {
	ev := parseFixture(t, "forgejo-issue_comment")
	got, err := ev.IssueComment()
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "created" || got.IsPullRequest || got.Issue.Number != 1 || !slices.Equal(got.Issue.Labels, []string{"bug"}) ||
		!slices.Equal(got.Issue.Assignees, []string{"bob"}) || got.Issue.Milestone == nil || got.Issue.Milestone.Title != "m1" {
		t.Errorf("event = %+v", got)
	}
	if got.Comment.ID == 0 || got.Comment.Body != "a comment" || got.Comment.Author != "gitopslib-admin" || got.Comment.HTMLURL == "" || got.Comment.CreatedAt.IsZero() {
		t.Errorf("comment = %+v", got.Comment)
	}

	// In a pull request's conversation: an issue_comment, unlike what Forgejo
	// calls pull_request_comment.
	ev = parseFixture(t, "forgejo-issue_comment-pull")
	if ev.Name != "issue_comment" || ev.Key != "" {
		t.Errorf("event = %+v", ev)
	}
	if got, err = ev.IssueComment(); err != nil {
		t.Fatal(err)
	}
	if !got.IsPullRequest || got.Issue.Number != 2 || got.Comment.Body != "pr conversation" {
		t.Errorf("event = %+v", got)
	}
}

func TestGitHubIssues(t *testing.T) {
	ev := parseFixture(t, "github-issues")
	if ev.Forge != gitoperationslib.BackendGitHub || ev.Name != "issues" || ev.Action != "opened" {
		t.Errorf("event = %+v", ev)
	}
	got, err := ev.Issues()
	if err != nil {
		t.Fatal(err)
	}
	if got.Issue.Number != 1 || got.Issue.State != gitoperationslib.IssueStateOpen || got.Issue.Author != "jeanfrancoisgratton" || got.Issue.HTMLURL == "" {
		t.Errorf("issue = %+v", got.Issue)
	}

	ev = parseFixture(t, "github-issue_comment")
	comment, err := ev.IssueComment()
	if err != nil {
		t.Fatal(err)
	}
	if comment.IsPullRequest || comment.Issue.Number != 1 || !slices.Equal(comment.Issue.Labels, []string{"bug"}) ||
		comment.Issue.Milestone == nil || comment.Issue.Milestone.ID != 1 || comment.Comment.ID == 0 || comment.Comment.Author != "jeanfrancoisgratton" {
		t.Errorf("event = %+v", comment)
	}
}

func TestForgejoReviews(t *testing.T) {
	tests := []struct {
		fixture, key, body string
		state              gitoperationslib.ReviewState
	}{
		{"forgejo-review-approved", "pull_request_approved", "lgtm", gitoperationslib.ReviewApproved},
		{"forgejo-review-rejected", "pull_request_rejected", "please fix", gitoperationslib.ReviewChangesRequested},
		{"forgejo-review-comment", "pull_request_comment", "note", gitoperationslib.ReviewCommented},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			ev := parseFixture(t, tt.fixture)
			if ev.Name != "pull_request_review" || ev.Key != tt.key || ev.Action != "submitted" || ev.Sender != "bob" {
				t.Errorf("event = %+v", ev)
			}
			got, err := ev.Review()
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != "submitted" || got.Number != 2 || got.PullRequest.Title != "PR one" || got.PullRequest.Head.Ref != "feat" {
				t.Errorf("review event = %+v", got)
			}
			if got.Review.Reviewer != "bob" || got.Review.State != tt.state || got.Review.Body != tt.body {
				t.Errorf("review = %+v", got.Review)
			}
			// Forgejo sends no separate event for line comments.
			if _, err := ev.ReviewComment(); err == nil {
				t.Error("ReviewComment() on a review delivery succeeded")
			}
		})
	}
}

func TestCreateAndDelete(t *testing.T) {
	tests := []struct {
		fixture, refType, ref, sha string
		get                        func(*Event) (*RefEvent, error)
	}{
		{"forgejo-create", "tag", "v1", "3c41a4432b1b8c998e01e11559dfad4108191b22", (*Event).Create},
		{"forgejo-delete-branch", "branch", "feat", "", (*Event).Delete},
		{"forgejo-delete-tag", "tag", "v1", "", (*Event).Delete},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got, err := tt.get(parseFixture(t, tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if got.RefType != tt.refType || got.Ref != tt.ref || got.SHA != tt.sha {
				t.Errorf("event = %+v", got)
			}
		})
	}
	if _, err := parseFixture(t, "forgejo-create").Delete(); err == nil {
		t.Error("Delete() on a create delivery succeeded")
	}
}

// bitbucketEvent decodes a payload following Bitbucket's documentation; no live
// delivery has been checked yet.
func bitbucketEvent(t *testing.T, key, body string) *Event {
	t.Helper()
	ev, err := Decode(http.Header{"X-Event-Key": {key}}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

const bbPullRequest = `"pullrequest":{"id":7,"title":"T","description":"D","state":"OPEN","author":{"nickname":"alice"},
	"source":{"branch":{"name":"feat"}},"destination":{"branch":{"name":"main"}},"links":{"html":{"href":"https://b/pr/7"}}},
	"repository":{"full_name":"acme/widget"},"actor":{"nickname":"bob"}`

func TestBitbucketReviews(t *testing.T) {
	tests := []struct {
		key, payload, action string
		state                gitoperationslib.ReviewState
		dated                bool
	}{
		{"pullrequest:approved", `"approval":{"date":"2026-11-05T10:00:00Z","user":{"nickname":"bob"}}`, "submitted", gitoperationslib.ReviewApproved, true},
		{"pullrequest:unapproved", `"approval":{"date":"2026-11-05T10:00:00Z","user":{"nickname":"bob"}}`, "dismissed", gitoperationslib.ReviewDismissed, true},
		{"pullrequest:changes_request_created", `"changes_request":{"date":"2026-11-05T10:00:00Z","user":{"nickname":"bob"}}`, "submitted", gitoperationslib.ReviewChangesRequested, true},
		{"pullrequest:changes_request_removed", `"x":1`, "dismissed", gitoperationslib.ReviewDismissed, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			ev := bitbucketEvent(t, tt.key, `{`+bbPullRequest+`,`+tt.payload+`}`)
			if ev.Name != "pull_request_review" {
				t.Errorf("Name = %q", ev.Name)
			}
			got, err := ev.Review()
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != tt.action || got.Number != 7 || got.Review.Reviewer != "bob" || got.Review.State != tt.state ||
				got.Review.SubmittedAt.IsZero() == tt.dated {
				t.Errorf("event = %+v", got)
			}
		})
	}
	if _, err := bitbucketEvent(t, "pullrequest:created", `{`+bbPullRequest+`}`).Review(); err == nil {
		t.Error("Review() on a created delivery succeeded")
	}
}

func TestBitbucketComments(t *testing.T) {
	const fields = `"id":9,"content":{"raw":"hello"},"user":{"nickname":"bob"},"created_on":"2026-11-05T10:00:00Z","links":{"html":{"href":"https://b/c/9"}}`

	ev := bitbucketEvent(t, "pullrequest:comment_updated", `{`+bbPullRequest+`,"comment":{`+fields+`}}`)
	if ev.Name != "issue_comment" || ev.Action != "edited" || ev.Key != "pullrequest:comment_updated" {
		t.Errorf("event = %+v", ev)
	}
	conversation, err := ev.IssueComment()
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.IsPullRequest || conversation.Issue.Number != 7 || conversation.Issue.Title != "T" || conversation.Comment.ID != 9 ||
		conversation.Comment.Body != "hello" || conversation.Comment.Author != "bob" {
		t.Errorf("comment event = %+v", conversation)
	}
	if _, err := ev.ReviewComment(); err == nil {
		t.Error("ReviewComment() on a conversation comment succeeded")
	}

	ev = bitbucketEvent(t, "pullrequest:comment_created", `{`+bbPullRequest+`,"comment":{`+fields+`,"inline":{"path":"a.go","to":12,"from":null}}}`)
	if ev.Name != "pull_request_review_comment" || ev.Action != "created" {
		t.Errorf("event = %+v", ev)
	}
	line, err := ev.ReviewComment()
	if err != nil {
		t.Fatal(err)
	}
	if line.Number != 7 || line.Comment.Path != "a.go" || line.Comment.Line != 12 || line.Comment.OldLine != 0 || line.Comment.Body != "hello" {
		t.Errorf("line comment event = %+v", line)
	}
	if _, err := ev.IssueComment(); err == nil {
		t.Error("IssueComment() on a line comment succeeded")
	}
	if ev = bitbucketEvent(t, "pullrequest:comment_resolved", `{`+bbPullRequest+`,"comment":{`+fields+`}}`); ev.Name != "pullrequest:comment_resolved" {
		t.Errorf("unmapped key: Name = %q", ev.Name)
	}
}

func TestBitbucketHasNoRefOrIssueEvents(t *testing.T) {
	ev := bitbucketEvent(t, "repo:push", `{"push":{"changes":[]}}`)
	if _, err := ev.Create(); err == nil {
		t.Error("Create() succeeded")
	}
	if _, err := ev.Delete(); err == nil {
		t.Error("Delete() succeeded")
	}
	if _, err := ev.Issues(); err == nil {
		t.Error("Issues() succeeded")
	}
}

// Following GitHub's documentation; item 99 in TODO.md is to replace these with
// captured deliveries.
func TestGitHubDocumentedPayloads(t *testing.T) {
	const pr = `"pull_request":{"number":5,"title":"T","state":"open","user":{"login":"alice"},"head":{"ref":"feat","sha":"abc"},"base":{"ref":"main"}},"repository":{"full_name":"acme/widget"},"sender":{"login":"bob"}`
	decode := func(event, body string) *Event {
		t.Helper()
		ev, err := Decode(http.Header{"X-Github-Event": {event}}, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	review, err := decode("pull_request_review", `{"action":"submitted",`+pr+`,"review":{"id":9,"user":{"login":"bob"},"body":"ok","state":"approved","commit_id":"abc","html_url":"https://g/r","submitted_at":"2026-11-05T10:00:00Z"}}`).Review()
	if err != nil {
		t.Fatal(err)
	}
	if review.Action != "submitted" || review.Number != 5 || review.Review.State != gitoperationslib.ReviewApproved || review.Review.Reviewer != "bob" ||
		review.Review.ID != 9 || review.Review.CommitID != "abc" || review.Review.SubmittedAt.IsZero() {
		t.Errorf("review = %+v", review)
	}

	line := func(side string, lineNo, original int) *ReviewCommentEvent {
		t.Helper()
		body := fmt.Sprintf(`{"action":"created",%s,"comment":{"id":3,"user":{"login":"bob"},"body":"b","path":"a.go","side":%q,"line":%d,"original_line":%d,"commit_id":"abc","pull_request_review_id":9}}`, pr, side, lineNo, original)
		got, err := decode("pull_request_review_comment", body).ReviewComment()
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := line("RIGHT", 4, 2); got.Number != 5 || got.Comment.Line != 4 || got.Comment.OldLine != 0 || got.Comment.Path != "a.go" || got.Comment.ReviewID != 9 {
		t.Errorf("new line = %+v", got)
	}
	if got := line("LEFT", 4, 2); got.Comment.OldLine != 4 || got.Comment.Line != 0 {
		t.Errorf("old line = %+v", got)
	}
	if got := line("RIGHT", 0, 2); got.Comment.Line != 2 { // outdated: only the original line
		t.Errorf("outdated = %+v", got)
	}

	made, err := decode("create", `{"ref":"feat","ref_type":"branch","master_branch":"main"}`).Create()
	if err != nil || made.Ref != "feat" || made.RefType != "branch" || made.SHA != "" {
		t.Errorf("create = %+v, %v", made, err)
	}
	gone, err := decode("delete", `{"ref":"v1","ref_type":"tag"}`).Delete()
	if err != nil || gone.Ref != "v1" || gone.RefType != "tag" {
		t.Errorf("delete = %+v, %v", gone, err)
	}
}

func TestWorkflowRun(t *testing.T) {
	ev := parseFixture(t, "forgejo-workflow_run-cancelled")
	if ev.Name != "workflow_run" || ev.Key != "workflow_run_cancelled" || ev.Action != "run_completed" ||
		ev.Repo != "gitopslib-admin/wf" || ev.Sender != "gitopslib-admin" {
		t.Errorf("event = %+v", ev)
	}
	got, err := ev.WorkflowRun()
	if err != nil {
		t.Fatal(err)
	}
	r := got.Run
	if got.Action != "run_completed" || r.ID != 4 || r.Number != 4 || r.Workflow != "c.yaml" || r.Event != "workflow_dispatch" || r.Branch != "main" ||
		r.SHA == "" || r.Status != "cancelled" || r.Conclusion != "cancelled" || r.Actor != "gitopslib-admin" || r.HTMLURL == "" ||
		r.CreatedAt.IsZero() || r.StartedAt.IsZero() || r.StoppedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Errorf("run = %+v", got)
	}
	if _, err := ev.WorkflowJob(); err == nil {
		t.Error("WorkflowJob() on a workflow_run delivery succeeded")
	}

	got, err = parseFixture(t, "forgejo-workflow_run-running").WorkflowRun()
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "running" || got.Run.Conclusion != "" || !got.Run.StoppedAt.IsZero() {
		t.Errorf("running = %+v", got)
	}

	ev = parseFixture(t, "github-workflow_run-completed")
	if ev.Name != "workflow_run" || ev.Key != "" || ev.Action != "completed" || ev.Repo == "" || ev.Sender == "" {
		t.Errorf("event = %+v", ev)
	}
	if got, err = ev.WorkflowRun(); err != nil {
		t.Fatal(err)
	}
	r = got.Run
	if r.ID == 0 || r.Number != 1 || r.Title != "add workflow" || r.Workflow != "w.yml" || r.Event != "push" || r.Branch == "" || r.SHA == "" ||
		r.Status != "completed" || r.Conclusion != "success" || r.Actor == "" || r.HTMLURL == "" || r.CreatedAt.IsZero() || r.StartedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Errorf("run = %+v", r)
	}
}

func TestWorkflowJob(t *testing.T) {
	ev := parseFixture(t, "forgejo-workflow_job-failure")
	if ev.Name != "workflow_job" || ev.Key != "workflow_job_failure" || ev.Action != "job_completed" || ev.Repo != "gitopslib-admin/wf" || ev.Sender != "gitopslib-admin" {
		t.Errorf("event = %+v", ev)
	}
	got, err := ev.WorkflowJob()
	if err != nil {
		t.Fatal(err)
	}
	j := got.Job
	if j.ID == 0 || j.RunID != 2 || j.Attempt != 1 || j.Name != "bad" || j.Status != "failure" || j.Conclusion != "failure" ||
		!slices.Equal(j.Labels, []string{"self"}) || j.HTMLURL == "" || got.Run == nil || got.Run.ID != 2 {
		t.Errorf("job = %+v", got)
	}
	if got, err = parseFixture(t, "forgejo-workflow_job-blocked").WorkflowJob(); err != nil {
		t.Fatal(err)
	}
	if got.Action != "new_job_attempt" || got.Job.Status != "blocked" || got.Job.Conclusion != "" {
		t.Errorf("blocked = %+v", got)
	}

	ev = parseFixture(t, "github-workflow_job-completed")
	if ev.Name != "workflow_job" || ev.Action != "completed" || ev.Repo == "" {
		t.Errorf("event = %+v", ev)
	}
	if got, err = ev.WorkflowJob(); err != nil {
		t.Fatal(err)
	}
	j = got.Job
	if j.ID == 0 || j.RunID == 0 || j.Attempt != 1 || j.Name != "ok" || j.Status != "completed" || j.Conclusion != "success" ||
		!slices.Equal(j.Labels, []string{"ubuntu-latest"}) || j.HTMLURL == "" || j.StartedAt.IsZero() || j.CompletedAt.IsZero() || got.Run != nil {
		t.Errorf("job = %+v", got)
	}
	if got, err = parseFixture(t, "github-workflow_job-queued").WorkflowJob(); err != nil {
		t.Fatal(err)
	}
	if got.Job.Status != "queued" || got.Job.Conclusion != "" || !got.Job.CompletedAt.IsZero() {
		t.Errorf("queued = %+v", got)
	}
}

// The older events keep their names, and now say which repository they are for.
func TestActionRunDelivery(t *testing.T) {
	ev := parseFixture(t, "forgejo-action_run_failure")
	if ev.Name != "action_run_failure" || ev.Key != "" || ev.Action != "failure" || ev.Repo != "gitopslib-admin/wf" || ev.Sender != "gitopslib-admin" {
		t.Errorf("event = %+v", ev)
	}
}
