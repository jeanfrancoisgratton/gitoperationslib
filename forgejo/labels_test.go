package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// labelServer has labels bug (1) and "needs review" (2), and milestone 3.
func labelServer(t *testing.T) (*Client, *[]issueWrite, *[]string) {
	t.Helper()
	var writes []issueWrite
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/acme/widget")
		requests = append(requests, r.Method+" "+path+"?"+r.URL.RawQuery)
		if r.Method != http.MethodGet {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, issueWrite{r.Method + " " + path, body})
		}
		const milestone = `{"id":3,"title":"v1","description":"d","state":"open","due_on":"2026-12-01T00:00:00Z","open_issues":2,"closed_issues":1}`
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && path == "/labels":
			io.WriteString(w, `[{"id":1,"name":"bug","color":"D73A4A"},{"id":2,"name":"needs review","color":"#ededed","description":"d"}]`)
		case strings.HasPrefix(path, "/labels"):
			io.WriteString(w, `{"id":2,"name":"needs review","color":"#ededed"}`)
		case r.Method == http.MethodGet && path == "/milestones":
			io.WriteString(w, "["+milestone+"]")
		case strings.HasPrefix(path, "/milestones"):
			io.WriteString(w, milestone)
		case path == "/issues/5":
			io.WriteString(w, issueResponse)
		case path == "/issues/5/comments":
			io.WriteString(w, `[{"id":9,"body":"hi","user":{"login":"bob"}}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	return c, &writes, &requests
}

func TestLabelsForgejo(t *testing.T) {
	c, writes, requests := labelServer(t)
	ctx := context.Background()
	labels := c.Labels()

	page, err := labels.List(ctx, "acme", "widget", gitoperationslib.ListOptions{Page: 2, PerPage: 5})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	// Colours come back in lower case, without "#".
	if page.Items[0].Color != "d73a4a" || page.Items[1].Color != "ededed" {
		t.Errorf("colours = %+v", page.Items)
	}
	if (*requests)[0] != "GET /labels?limit=5&page=2" {
		t.Errorf("request = %s", (*requests)[0])
	}

	// Forgejo addresses labels by ID: names are looked up in the list.
	if got, err := labels.Get(ctx, "acme", "widget", "needs review"); err != nil || got.ID != 2 || got.Description != "d" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := labels.Get(ctx, "acme", "widget", "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(none): err = %v, want 404", err)
	}

	if _, err := labels.Edit(ctx, "acme", "widget", "needs review", gitoperationslib.EditLabelOptions{
		Name: gitoperationslib.Ptr("reviewing"), Color: gitoperationslib.Ptr("FFFFFF"), Description: gitoperationslib.Ptr(""),
	}); err != nil {
		t.Fatal(err)
	}
	want := issueWrite{"PATCH /labels/2", map[string]any{"name": "reviewing", "color": "#ffffff", "description": ""}}
	if !reflect.DeepEqual((*writes)[0], want) {
		t.Errorf("Edit write = %+v\nwant         %+v", (*writes)[0], want)
	}
	if err := labels.Delete(ctx, "acme", "widget", "bug"); err != nil || (*writes)[1].request != "DELETE /labels/1" {
		t.Errorf("Delete: %v, %+v", err, *writes)
	}

	*writes = nil
	if _, err := labels.Edit(ctx, "acme", "widget", "none", gitoperationslib.EditLabelOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Edit(none): err = %v, want 404", err)
	}
	if err := labels.Delete(ctx, "acme", "widget", "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Delete(none): err = %v, want 404", err)
	}
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{}); err == nil {
		t.Error("label without a name accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", *writes)
	}

	// Forgejo requires a colour: a default fills in.
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[0].body["color"]; got != "#ededed" {
		t.Errorf("default colour = %v", got)
	}
	if _, err := labels.Create(ctx, "acme", "widget", gitoperationslib.Label{Name: "y", Color: "#D73A4A"}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[1].body["color"]; got != "#d73a4a" {
		t.Errorf("colour = %v", got)
	}
}

func TestMilestonesForgejo(t *testing.T) {
	c, writes, requests := labelServer(t)
	ctx := context.Background()
	ms := c.Milestones()
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	page, err := ms.List(ctx, "acme", "widget", gitoperationslib.MilestoneStateClosed, gitoperationslib.ListOptions{PerPage: 5})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.ID != 3 || got.Title != "v1" || !got.DueOn.Equal(due) || got.OpenIssues != 2 || got.ClosedIssues != 1 {
		t.Errorf("milestone = %+v", got)
	}
	// No state: the server's default applies.
	if _, err := ms.List(ctx, "acme", "widget", "", gitoperationslib.ListOptions{}); err != nil {
		t.Error(err)
	}
	if !slices.Equal((*requests)[:2], []string{"GET /milestones?limit=5&page=1&state=closed", "GET /milestones?"}) {
		t.Errorf("requests = %v", (*requests)[:2])
	}
	if got, err := ms.Get(ctx, "acme", "widget", 3); err != nil || got.ID != 3 {
		t.Errorf("Get = %+v, %v", got, err)
	}

	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{Title: "new", Description: "d", DueOn: due}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "new", "description": "d", "due_on": "2026-12-01T00:00:00Z"}
	if !reflect.DeepEqual((*writes)[0].body, want) {
		t.Errorf("Create body = %v", (*writes)[0].body)
	}
	// No date, no due_on.
	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{Title: "v2"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual((*writes)[1].body, map[string]any{"title": "v2"}) {
		t.Errorf("minimal Create body = %v", (*writes)[1].body)
	}

	closed := gitoperationslib.MilestoneStateClosed
	if _, err := ms.Edit(ctx, "acme", "widget", 3, gitoperationslib.EditMilestoneOptions{State: &closed}); err != nil {
		t.Fatal(err)
	}
	if got := (*writes)[2]; got.request != "PATCH /milestones/3" || !reflect.DeepEqual(got.body, map[string]any{"state": "closed"}) {
		t.Errorf("Edit write = %+v", got)
	}
	if err := ms.Delete(ctx, "acme", "widget", 3); err != nil || (*writes)[3].request != "DELETE /milestones/3" {
		t.Errorf("Delete: %v, %+v", err, *writes)
	}

	*writes = nil
	if _, err := ms.Create(ctx, "acme", "widget", gitoperationslib.CreateMilestoneOptions{}); err == nil {
		t.Error("milestone without a title accepted")
	}
	if len(*writes) != 0 {
		t.Errorf("refused call sent %v", *writes)
	}
}

func TestIssueGetAndCommentsForgejo(t *testing.T) {
	c, _, requests := labelServer(t)
	ctx := context.Background()
	issue, err := c.Issues().Get(ctx, "acme", "widget", 5)
	if err != nil || issue.Number != 5 || issue.Author != "alice" || issue.Milestone == nil {
		t.Errorf("Get = %+v, %v", issue, err)
	}
	// Forgejo does not paginate comments: no paging is sent.
	page, err := c.Issues().ListComments(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{Page: 2, PerPage: 5})
	if err != nil || len(page.Items) != 1 || page.Items[0].Author != "bob" {
		t.Errorf("ListComments = %+v, %v", page, err)
	}
	if last := (*requests)[len(*requests)-1]; last != "GET /issues/5/comments?" {
		t.Errorf("request = %s", last)
	}
}

func TestDirectoryListAllOrgsAndTeamReposForgejo(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/v1/orgs":
			io.WriteString(w, "["+orgResponse+"]")
		case "/api/v1/orgs/acme/teams/search":
			io.WriteString(w, `{"ok": true, "data": [`+teamResponse+`]}`)
		case "/api/v1/teams/6/repos":
			io.WriteString(w, "["+repoResponse+"]")
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	orgs, err := c.Orgs().ListAll(ctx, gitoperationslib.ListOptions{PerPage: 5})
	if err != nil || len(orgs.Items) != 1 || orgs.Items[0].Name != "acme" || requests[0] != "/api/v1/orgs?limit=5&page=1" {
		t.Errorf("ListAllOrgs = %+v, %v, %v", orgs, err, requests)
	}
	repos, err := c.Teams().Repos(ctx, "acme", "devs", gitoperationslib.ListOptions{})
	if err != nil || len(repos.Items) != 1 {
		t.Errorf("TeamRepos = %+v, %v", repos, err)
	}
	if _, err := c.Teams().Repos(ctx, "acme", "nobody", gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("TeamRepos(nobody): err = %v, want 404", err)
	}
}

func TestListSystemWebhooksForgejo(t *testing.T) {
	c, _ := fakeSiteHooks(t)
	page, err := c.Site().SystemWebhooks().List(context.Background(), gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 2 || page.NextPage != 2 {
		t.Errorf("List = %+v, %v", page, err)
	}
}

func TestClientOptionsForgejo(t *testing.T) {
	var agents []string
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"version":"15.0.2+gitea-1.22.0"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		agents = append(agents, r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"message":"down"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), srv.URL, Token("pat"), WithHTTPClient(srv.Client()), WithUserAgent("gitclt/1"), WithRetry(gitoperationslib.NoRetry))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Repos().Get(context.Background(), "acme", "widget")
	if gitoperationslib.StatusCode(err) != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want the 503", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d: NoRetry must not retry", attempts)
	}
	if fmt.Sprint(agents) != "[gitclt/1]" {
		t.Errorf("User-Agent = %v", agents)
	}
}
