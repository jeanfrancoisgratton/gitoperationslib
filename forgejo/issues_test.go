package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const issueResponse = `{
	"number": 5, "title": "Bug", "body": "b", "state": "open", "user": {"login": "alice"},
	"labels": [{"id": 1, "name": "bug", "color": "D73A4A"}], "assignees": [{"login": "bob"}],
	"milestone": {"id": 3, "title": "v1.0", "state": "open", "due_on": "2026-12-01T00:00:00Z"},
	"comments": 2, "is_locked": false, "html_url": "https://h/acme/widget/issues/5",
	"created_at": "2026-10-17T10:00:00Z", "updated_at": "2026-10-17T11:00:00Z", "closed_at": null
}`

func TestIssueConversion(t *testing.T) {
	var j issueJSON
	if err := json.Unmarshal([]byte(issueResponse), &j); err != nil {
		t.Fatal(err)
	}
	i := j.toIssue()
	if i.Number != 5 || i.Author != "alice" || fmt.Sprint(i.Labels) != "[bug]" || fmt.Sprint(i.Assignees) != "[bob]" ||
		i.Milestone == nil || i.Milestone.ID != 3 || i.Milestone.DueOn.Month() != time.December || i.Comments != 2 || !i.ClosedAt.IsZero() {
		t.Errorf("got %+v", i)
	}
	if got := (&labelJSON{Color: "#D73A4A"}).toLabel().Color; got != "d73a4a" {
		t.Errorf("colour = %q", got)
	}
}

func TestIssuesRequests(t *testing.T) {
	var requests []string
	var bodies []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, strings.TrimSpace(string(data)))
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/repos/acme/widget/labels" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":1,"name":"bug","color":"d73a4a"},{"id":2,"name":"triage","color":"ededed"}]`)
		case strings.Contains(r.URL.Path, "/issues/") && strings.HasSuffix(r.URL.Path, "/labels"):
			io.WriteString(w, `[]`)
		case strings.Contains(r.URL.Path, "/labels"):
			io.WriteString(w, `{"id":2,"name":"needs-triage","color":"#fbca04"}`)
		case strings.HasSuffix(r.URL.Path, "/comments") && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":9,"body":"hi","user":{"login":"bob"}}]`)
		case strings.Contains(r.URL.Path, "/comments"):
			io.WriteString(w, `{"id":9,"body":"hi","user":{"login":"bob"}}`)
		case strings.Contains(r.URL.Path, "/milestones"):
			io.WriteString(w, `{"id":3,"title":"v1.0","state":"open"}`)
		case r.URL.Path == "/api/v1/repos/acme/widget/issues" && r.Method == http.MethodGet:
			io.WriteString(w, "["+issueResponse+"]")
		default:
			io.WriteString(w, issueResponse)
		}
	})
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	is, lb, ms := c.Issues(), c.Labels(), c.Milestones()

	_, err := is.List(ctx, "acme", "widget", gitoperationslib.ListIssuesOptions{State: gitoperationslib.IssueStateAll, Labels: []string{"bug", "triage"}})
	check(err)
	_, err = is.Create(ctx, "acme", "widget", gitoperationslib.CreateIssueOptions{Title: "Bug", Labels: []string{"triage", "bug"}, Assignees: []string{"bob"}, Milestone: 3})
	check(err)
	_, err = is.Edit(ctx, "acme", "widget", 5, gitoperationslib.EditIssueOptions{Labels: &[]string{"bug"}, Assignees: &[]string{}, Milestone: gitoperationslib.Ptr(int64(0))})
	check(err)
	check(is.AddLabels(ctx, "acme", "widget", 5, []string{"triage"}))
	check(is.RemoveLabel(ctx, "acme", "widget", 5, "triage"))
	if err := is.AddLabels(ctx, "acme", "widget", 5, []string{"nope"}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown label: err = %v", err)
	}
	_, err = is.CreateComment(ctx, "acme", "widget", 5, "hi")
	check(err)
	_, err = is.EditComment(ctx, "acme", "widget", 9, "hello")
	check(err)
	check(is.DeleteComment(ctx, "acme", "widget", 9))
	check(is.Delete(ctx, "acme", "widget", 5))
	_, err = lb.Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "x"})
	check(err)
	_, err = lb.Edit(ctx, "acme", "widget", "triage", gitoperationslib.EditLabelOptions{Color: gitoperationslib.Ptr("FBCA04")})
	check(err)
	_, err = ms.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditMilestoneOptions{State: gitoperationslib.Ptr(gitoperationslib.MilestoneStateClosed)})
	check(err)

	want := []string{
		"GET /api/v1/repos/acme/widget/issues?labels=bug%2Ctriage&state=all&type=issues ",
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		`POST /api/v1/repos/acme/widget/issues? {"title":"Bug","labels":[2,1],"assignees":["bob"],"milestone":3}`,
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		`PUT /api/v1/repos/acme/widget/issues/5/labels? {"labels":[1]}`,
		`PATCH /api/v1/repos/acme/widget/issues/5? {"assignees":[],"milestone":0}`,
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		`POST /api/v1/repos/acme/widget/issues/5/labels? {"labels":[2]}`,
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		"DELETE /api/v1/repos/acme/widget/issues/5/labels/2? ",
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		`POST /api/v1/repos/acme/widget/issues/5/comments? {"body":"hi"}`,
		`PATCH /api/v1/repos/acme/widget/issues/comments/9? {"body":"hello"}`,
		"DELETE /api/v1/repos/acme/widget/issues/comments/9? ",
		"DELETE /api/v1/repos/acme/widget/issues/5? ",
		`POST /api/v1/repos/acme/widget/labels? {"color":"#ededed","description":"","name":"x"}`,
		"GET /api/v1/repos/acme/widget/labels?limit=50&page=1 ",
		`PATCH /api/v1/repos/acme/widget/labels/2? {"color":"#fbca04"}`,
		`PATCH /api/v1/repos/acme/widget/milestones/3? {"state":"closed"}`,
	}
	var got []string
	for i := range requests {
		got = append(got, requests[i]+" "+bodies[i])
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
