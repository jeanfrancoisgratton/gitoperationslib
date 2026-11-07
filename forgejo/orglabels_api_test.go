package forgejo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestListOrgLabelsUserAndNobody(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/alice" {
			io.WriteString(w, `{"login":"alice"}`)
			return
		}
		http.NotFound(w, r)
	})
	ctx := context.Background()
	page, err := c.ListOrgLabels(ctx, "alice", gitoperationslib.ListOptions{})
	if err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Errorf("a user: %+v, %v", page, err)
	}
	if _, err := c.ListOrgLabels(ctx, "nobody", gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("nobody: %v", err)
	}
}

func TestLabelNames(t *testing.T) {
	var writes []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method)
		}
		io.WriteString(w, `[{"id":1,"name":"dup"},{"id":2,"name":"dup"},{"id":3,"name":"single"}]`)
	})
	ctx := context.Background()
	labels := c.Labels()
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "single"}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
		t.Errorf("Create: %v", err)
	}
	if _, err := labels.Edit(ctx, "acme", "widget", "single", gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("dup")}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
		t.Errorf("rename: %v", err)
	}
	_, err := labels.Get(ctx, "acme", "widget", "dup")
	if !errors.Is(err, gitoperationslib.ErrAmbiguousName) || !strings.Contains(err.Error(), "[1 2]") {
		t.Errorf("Get: %v", err)
	}
	if len(writes) != 0 {
		t.Errorf("refusals wrote: %v", writes)
	}
}

func TestPullRequestNumberRefused(t *testing.T) {
	var writes []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method)
		}
		io.WriteString(w, `{"number":7,"state":"open","pull_request":{"merged":false},"labels":[]}`)
	})
	ctx := context.Background()
	got, err := c.Issues().Get(ctx, "acme", "widget", 7)
	if err != nil || !got.IsPullRequest {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if err := c.Issues().Delete(ctx, "acme", "widget", 7); !errors.Is(err, gitoperationslib.ErrIsPullRequest) {
		t.Errorf("Delete: %v", err)
	}
	closed := gitoperationslib.IssueStateClosed
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 7, gitoperationslib.EditIssueOptions{State: &closed}); !errors.Is(err, gitoperationslib.ErrIsPullRequest) {
		t.Errorf("Edit: %v", err)
	}
	if len(writes) != 0 {
		t.Errorf("refusals wrote: %v", writes)
	}
	// Other edits need no check, and go through.
	title := "t"
	if _, err := c.Issues().Edit(ctx, "acme", "widget", 7, gitoperationslib.EditIssueOptions{Title: &title}); err != nil || len(writes) != 1 {
		t.Errorf("Edit(Title): %v, writes %v", err, writes)
	}
}

func TestUnknownLabelFilterAsksNoIssues(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			t.Errorf("asked the server: %s", r.URL)
		}
		io.WriteString(w, `[{"id":1,"name":"bug"}]`)
	})
	page, err := c.Issues().List(context.Background(), "acme", "widget", gitoperationslib.ListIssuesOptions{Labels: []string{"bug", "nope"}})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextPage != 0 {
		t.Errorf("List = %+v, %v", page, err)
	}
}

func TestClearDueOnForgejo(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s %s", r.Method, r.URL) })
	_, err := c.Milestones().Edit(context.Background(), "acme", "widget", 3, gitoperationslib.EditMilestoneOptions{ClearDueOn: true})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("err = %v", err)
	}
}
