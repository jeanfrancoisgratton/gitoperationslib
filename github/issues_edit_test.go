package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

type issueWrite struct {
	request string
	body    map[string]any
}

// issueServer answers every issue, label, milestone and comment request, and
// records the writes with their decoded bodies.
func issueServer(t *testing.T) (*Client, *[]issueWrite, *[]string) {
	t.Helper()
	var writes []issueWrite
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v3/repos/acme/widget")
		requests = append(requests, r.Method+" "+path+"?"+r.URL.RawQuery)
		if r.Method != http.MethodGet {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, issueWrite{r.Method + " " + path, body})
		}
		const comment = `{"id":9,"body":"hi","user":{"login":"bob"},"html_url":"https://h/c9","created_at":"2026-10-17T10:00:00Z"}`
		const label = `{"id":1,"name":"bug","color":"D73A4A","description":"d"}`
		const milestone = `{"number":3,"title":"v1","description":"d","state":"open","due_on":"2026-12-01T00:00:00Z","open_issues":2,"closed_issues":1}`
		switch {
		case r.Method == http.MethodDelete:
			if strings.Contains(path, "/issues/") && strings.Contains(path, "/labels/") {
				io.WriteString(w, `[]`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && path == "/labels" && r.URL.Query().Get("per_page") == "100":
			// The library checking that labels exist.
			io.WriteString(w, `[`+label+`,{"id":2,"name":"ui"}]`)
		case r.Method == http.MethodGet && path == "/labels":
			w.Header().Set("Link", `<https://x/?page=2>; rel="next"`)
			io.WriteString(w, "["+label+"]")
		case strings.HasPrefix(path, "/labels"):
			io.WriteString(w, label)
		case r.Method == http.MethodGet && path == "/milestones":
			io.WriteString(w, "["+milestone+"]")
		case strings.HasPrefix(path, "/milestones"):
			io.WriteString(w, milestone)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/comments"):
			w.Header().Set("Link", `<https://x/?page=3>; rel="next"`)
			io.WriteString(w, "["+comment+"]")
		case strings.Contains(path, "/comments"):
			io.WriteString(w, comment)
		case strings.HasSuffix(path, "/labels"):
			io.WriteString(w, "["+label+"]")
		default:
			io.WriteString(w, `{"number":1,"title":"Bug","state":"open","user":{"login":"alice"},"assignees":[{"login":"bob"}],"comments":4}`)
		}
	})
	return c, &writes, &requests
}

func TestCreateIssueGitHub(t *testing.T) {
	c, writes, _ := issueServer(t)
	ctx := context.Background()
	issue, err := c.Issues().Create(ctx, "acme", "widget", gitoperationslib.CreateIssueOptions{
		Title: "Bug", Body: "B", Labels: []string{"bug"}, Assignees: []string{"bob"}, Milestone: 3,
	})
	if err != nil || issue.Number != 1 || issue.Author != "alice" || !slices.Equal(issue.Assignees, []string{"bob"}) || issue.Comments != 4 {
		t.Fatalf("Create = %+v, %v", issue, err)
	}
	want := map[string]any{"title": "Bug", "body": "B", "labels": []any{"bug"}, "assignees": []any{"bob"}, "milestone": float64(3)}
	if !reflect.DeepEqual((*writes)[0].body, want) {
		t.Errorf("body = %v\nwant   %v", (*writes)[0].body, want)
	}

	// Empty fields are left out.
	*writes = nil
	if _, err := c.Issues().Create(ctx, "acme", "widget", gitoperationslib.CreateIssueOptions{Title: "Bug"}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[0].body; !reflect.DeepEqual(got, map[string]any{"title": "Bug"}) {
		t.Errorf("minimal body = %v", got)
	}

	*writes = nil
	if _, err := c.Issues().Create(ctx, "acme", "widget", gitoperationslib.CreateIssueOptions{}); err == nil {
		t.Error("issue without a title accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused call sent %v", *writes)
	}
}

func TestEditIssueGitHub(t *testing.T) {
	c, writes, _ := issueServer(t)
	ctx := context.Background()
	closed, bad := gitoperationslib.IssueStateClosed, gitoperationslib.IssueState("bad")

	// Nil fields are left out, so the server leaves them unchanged.
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Title: gitoperationslib.Ptr("New"), State: &closed}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[0]; got.request != "PATCH /issues/1" || !reflect.DeepEqual(got.body, map[string]any{"title": "New", "state": "closed"}) {
		t.Errorf("write = %+v", got)
	}

	// The documented way to clear labels and assignees: an empty slice is sent.
	*writes = nil
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Labels: &[]string{}, Assignees: &[]string{}}); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"labels": []any{}, "assignees": []any{}}; !reflect.DeepEqual((*writes)[0].body, want) {
		t.Errorf("clearing: body = %v, want %v", (*writes)[0].body, want)
	}
	// A nil slice behind the pointer clears too, instead of being dropped.
	*writes = nil
	var none []string
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Labels: &none}); err != nil {
		t.Fatal(err)
	}
	if got, ok := (*writes)[0].body["labels"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("nil slice: body = %v", (*writes)[0].body)
	}

	// Setting a milestone is one request.
	*writes = nil
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Milestone: gitoperationslib.Ptr(int64(5))}); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].body["milestone"] != float64(5) {
		t.Errorf("setting a milestone: %+v", *writes)
	}

	// Removing it needs an explicit null, which only the raw request can send.
	*writes = nil
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Milestone: gitoperationslib.Ptr(int64(0))}); err != nil {
		t.Fatal(err)
	}
	if v, ok := (*writes)[0].body["milestone"]; !ok || v != nil {
		t.Errorf("removing a milestone: first body = %v, want milestone: null", (*writes)[0].body)
	}

	*writes = nil
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{State: &bad}); err == nil {
		t.Error("invalid state accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused call sent %v", *writes)
	}
}

func TestIssueLabelsAndCommentsGitHub(t *testing.T) {
	c, writes, _ := issueServer(t)
	ctx := context.Background()
	is := c.Issues()

	if err := is.AddLabels(ctx, "acme", "widget", 1, []string{"bug", "ui"}); err != nil {
		t.Error(err)
	}
	if err := is.RemoveLabel(ctx, "acme", "widget", 1, "needs review"); err != nil {
		t.Error(err)
	}
	page, err := is.ListComments(ctx, "acme", "widget", 1, gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.NextPage != 3 {
		t.Fatalf("ListComments = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.ID != 9 || got.Author != "bob" || got.Body != "hi" || got.HTMLURL != "https://h/c9" || got.CreatedAt.IsZero() {
		t.Errorf("comment = %+v", got)
	}
	if got, err := is.CreateComment(ctx, "acme", "widget", 1, "hi"); err != nil || got.ID != 9 {
		t.Errorf("CreateComment = %+v, %v", got, err)
	}
	if got, err := is.EditComment(ctx, "acme", "widget", 9, "edited"); err != nil || got.ID != 9 {
		t.Errorf("EditComment = %+v, %v", got, err)
	}
	if err := is.DeleteComment(ctx, "acme", "widget", 9); err != nil {
		t.Error(err)
	}

	var got []string
	for _, w := range *writes {
		got = append(got, w.request)
	}
	want := []string{
		"POST /issues/1/labels",
		"DELETE /issues/1/labels/needs%20review",
		"POST /issues/1/comments",
		"PATCH /issues/comments/9",
		"DELETE /issues/comments/9",
	}
	if !slices.Equal(got, want) {
		t.Errorf("writes = %v\nwant     %v", got, want)
	}
	if b := (*writes)[3].body; b["body"] != "edited" {
		t.Errorf("EditComment body = %v", b)
	}
}

func TestLabelsGitHub(t *testing.T) {
	c, writes, requests := issueServer(t)
	ctx := context.Background()
	labels := c.Labels()

	page, err := labels.List(ctx, "acme", "widget", gitoperationslib.ListOptions{Page: 2, PerPage: 5})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	// Colours are reported in lower case.
	if got := page.Items[0]; got != (gitoperationslib.Label{ID: 1, Name: "bug", Color: "d73a4a", Description: "d"}) {
		t.Errorf("label = %+v", got)
	}
	if !strings.Contains((*requests)[0], "page=2") || !strings.Contains((*requests)[0], "per_page=5") {
		t.Errorf("request = %s", (*requests)[0])
	}
	if got, err := labels.Get(ctx, "acme", "widget", "needs review"); err != nil || got.Name != "bug" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if !strings.HasPrefix((*requests)[1], "GET /labels/needs%20review") {
		t.Errorf("request = %s", (*requests)[1])
	}

	// A leading # is dropped from colours, and a new name goes in new_name.
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "bug", Color: "#D73A4A", Description: "d"}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[0].body; !reflect.DeepEqual(got, map[string]any{"name": "bug", "color": "D73A4A", "description": "d"}) {
		t.Errorf("Create body = %v", got)
	}
	if _, err := labels.Edit(ctx, "acme", "widget", "bug", gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("defect"), Color: gitoperationslib.Ptr("#FFFFFF")}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[1]; got.request != "PATCH /labels/bug" || !reflect.DeepEqual(got.body, map[string]any{"new_name": "defect", "color": "FFFFFF"}) {
		t.Errorf("Edit write = %+v", got)
	}
	if err := labels.Delete(ctx, "acme", "widget", "bug"); err != nil || (*writes)[2].request != "DELETE /labels/bug" {
		t.Errorf("Delete: %v, %+v", err, (*writes)[2])
	}

	*writes = nil
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{}); err == nil {
		t.Error("label without a name accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused call sent %v", *writes)
	}
}

func TestMilestonesGitHub(t *testing.T) {
	c, writes, requests := issueServer(t)
	ctx := context.Background()
	ms := c.Milestones()
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	page, err := ms.List(ctx, "acme", "widget", gitoperationslib.MilestoneStateAll, gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	// The ID is GitHub's milestone number, which addresses it.
	got := page.Items[0]
	if got.ID != 3 || got.Title != "v1" || got.State != gitoperationslib.MilestoneStateOpen || !got.DueOn.Equal(due) || got.OpenIssues != 2 || got.ClosedIssues != 1 {
		t.Errorf("milestone = %+v", got)
	}
	if !strings.Contains((*requests)[0], "state=all") {
		t.Errorf("request = %s", (*requests)[0])
	}
	if m, err := ms.Get(ctx, "acme", "widget", 3); err != nil || m.ID != 3 || !strings.HasPrefix((*requests)[1], "GET /milestones/3") {
		t.Errorf("Get = %+v, %v, %s", m, err, (*requests)[1])
	}

	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{Title: "v1", Description: "d", DueOn: due}); err != nil {
		t.Fatal(err)
	}
	if b := (*writes)[0].body; b["title"] != "v1" || b["description"] != "d" || b["due_on"] != "2026-12-01T00:00:00Z" {
		t.Errorf("Create body = %v", b)
	}
	// No date, no due_on.
	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{Title: "v2"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*writes)[1].body["due_on"]; ok {
		t.Errorf("zero date sent: %v", (*writes)[1].body)
	}

	closed := gitoperationslib.MilestoneStateClosed
	if _, err := ms.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditMilestoneOptions{State: &closed, DueOn: &due}); err != nil {
		t.Fatal(err)
	}
	if w := (*writes)[2]; w.request != "PATCH /milestones/3" || w.body["state"] != "closed" || w.body["due_on"] != "2026-12-01T00:00:00Z" {
		t.Errorf("Edit write = %+v", w)
	}
	if err := ms.Delete(ctx, "acme", "widget", 3); err != nil || (*writes)[3].request != "DELETE /milestones/3" {
		t.Errorf("Delete: %v, %+v", err, (*writes)[3])
	}

	*writes = nil
	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{}); err == nil {
		t.Error("milestone without a title accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused call sent %v", *writes)
	}
}

func TestIssueErrorsGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	})
	ctx := context.Background()
	for name, err := range map[string]error{
		"Issues.Get":       second(c.Issues().Get(ctx, "acme", "widget", 1)),
		"Issues.Delete":    c.Issues().Delete(ctx, "acme", "widget", 1),
		"AddLabels":        c.Issues().AddLabels(ctx, "acme", "widget", 1, []string{"x"}),
		"CreateComment":    second(c.Issues().CreateComment(ctx, "acme", "widget", 1, "x")),
		"Labels.Get":       second(c.Labels().Get(ctx, "acme", "widget", "x")),
		"Milestones.Get":   second(c.Milestones().Get(ctx, "acme", "widget", 1)),
		"Milestones.Edit":  second(c.Milestones().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditMilestoneOptions{})),
		"remove milestone": second(c.Issues().Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{Milestone: gitoperationslib.Ptr(int64(0))})),
	} {
		if !gitoperationslib.IsNotFound(err) {
			t.Errorf("%s: err = %v (%s), want 404", name, err, fmt.Sprintf("%T", err))
		}
	}
}

func second[T any](_ T, err error) error { return err }
