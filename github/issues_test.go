package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestIssuesGitHub(t *testing.T) {
	var writes []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Method != http.MethodGet {
			data, _ := io.ReadAll(r.Body)
			writes = append(writes, r.Method+" "+p+" "+strings.TrimSpace(string(data)))
		}
		switch {
		case p == "/api/v3/repos/acme/widget/issues" && r.Method == http.MethodGet:
			if r.URL.Query().Get("labels") != "bug,triage" || r.URL.Query().Get("state") != "all" {
				t.Errorf("list query = %s", r.URL.RawQuery)
			}
			// Issue 1, and pull request 2 (which must be dropped).
			io.WriteString(w, `[{"number":1,"title":"Bug","labels":[{"name":"bug","color":"D73A4A"}]},
				{"number":2,"title":"PR","pull_request":{"url":"x"}}]`)
		case p == "/api/graphql":
			io.WriteString(w, `{"data":{"deleteIssue":{"clientMutationId":null}}}`)
		case p == "/api/v3/repos/acme/widget/labels" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":1,"name":"bug","color":"D73A4A"}`)
		default:
			io.WriteString(w, `{"number":1,"node_id":"I_node1","title":"Bug","milestone":{"number":3,"title":"v1"}}`)
		}
	})
	ctx := context.Background()
	is := c.Issues()

	page, err := is.List(ctx, "acme", "widget", gitoperationslib.ListIssuesOptions{State: gitoperationslib.IssueStateAll, Labels: []string{"bug", "triage"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].Number != 1 {
		t.Fatalf("List = %+v, %v (the pull request must be excluded)", page, err)
	}
	if issue, err := is.Get(ctx, "acme", "widget", 1); err != nil || issue.Milestone == nil || issue.Milestone.ID != 3 {
		t.Errorf("Get = %+v, %v (milestone ID is GitHub's milestone number)", issue, err)
	}
	if _, err := is.Edit(ctx, "acme", "widget", 1, gitoperationslib.EditIssueOptions{
		Labels: &[]string{}, Milestone: gitoperationslib.Ptr(int64(0)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := is.Delete(ctx, "acme", "widget", 1); err != nil {
		t.Fatal(err)
	}
	lb, err := c.Labels().Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "bug", Color: "#D73A4A"})
	if err != nil || lb.Color != "d73a4a" {
		t.Errorf("label = %+v, %v", lb, err)
	}

	if len(writes) != 4 {
		t.Fatalf("writes = %v", writes)
	}
	// Clearing the milestone needs an explicit null; labels []string{} clears them.
	if writes[0] != `PATCH /api/v3/repos/acme/widget/issues/1 {"milestone":null}` {
		t.Errorf("milestone clear = %s", writes[0])
	}
	if !strings.HasPrefix(writes[1], `PATCH /api/v3/repos/acme/widget/issues/1 {"labels":[]`) {
		t.Errorf("edit = %s", writes[1])
	}
	var gql map[string]any
	json.Unmarshal([]byte(strings.TrimPrefix(writes[2], "POST /api/graphql ")), &gql)
	if !strings.Contains(gql["query"].(string), "deleteIssue") || gql["variables"].(map[string]any)["id"] != "I_node1" {
		t.Errorf("delete = %s", writes[2])
	}
	if writes[3] != `POST /api/v3/repos/acme/widget/labels {"name":"bug","color":"D73A4A"}` {
		t.Errorf("label create = %s", writes[3])
	}
}
