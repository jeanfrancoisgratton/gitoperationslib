package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestHookEventTranslation(t *testing.T) {
	in := []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequestReview, "package"}
	got := toForgejoEvents(in)
	want := []string{"push", "pull_request_review_approved", "pull_request_review_rejected", "pull_request_review_comment", "package"}
	if !slices.Equal(got, want) {
		t.Errorf("toForgejoEvents = %v", got)
	}
	back := fromForgejoEvents(append(got, "issue_assign"))
	if fmt.Sprint(back) != "[push pull_request_review package issue_assign]" {
		t.Errorf("fromForgejoEvents = %v", back)
	}
}

func TestWebhooksRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete, strings.HasSuffix(r.URL.Path, "/tests"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			io.WriteString(w, `[{"id":7,"config":{"url":"https://h/x","content_type":"json"},"events":["push"],"active":true}]`)
		default:
			// Echo the requested events, as Forgejo does for known ones.
			events, _ := json.Marshal(body["events"])
			if body["events"] == nil {
				events = []byte(`["push"]`)
			}
			fmt.Fprintf(w, `{"id":7,"config":{"url":"https://h/x","content_type":"form"},"events":%s,"active":false,"branch_filter":"main"}`, events)
		}
	})
	ctx := context.Background()
	hooks := c.Webhooks()
	repo, org := gitoperationslib.RepoHooks("acme", "widget"), gitoperationslib.OrgHooks("acme")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	list, err := hooks.List(ctx, repo, gitoperationslib.ListOptions{})
	check(err)
	if len(list.Items) != 1 || list.Items[0].URL != "https://h/x" {
		t.Errorf("List = %+v", list)
	}
	_, err = hooks.List(ctx, org, gitoperationslib.ListOptions{})
	check(err)
	_, err = hooks.Get(ctx, repo, 7)
	check(err)
	created, err := hooks.Create(ctx, repo, gitoperationslib.CreateWebhookOptions{
		URL: "https://h/x", Secret: "s", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequestReview},
		BranchFilter: "main", AuthorizationHeader: "Bearer x",
	})
	check(err)
	if !slices.Equal(created.Events, []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequestReview}) ||
		created.ContentType != gitoperationslib.HookContentForm || created.BranchFilter != "main" {
		t.Errorf("created = %+v", created)
	}
	_, err = hooks.Edit(ctx, org, 7, gitoperationslib.EditWebhookOptions{Secret: gitoperationslib.Ptr("new"), Active: gitoperationslib.Ptr(false)})
	check(err)
	check(hooks.Ping(ctx, repo, 7))
	check(hooks.Delete(ctx, org, 7))

	wantRequests := []string{
		"GET /api/v1/repos/acme/widget/hooks",
		"GET /api/v1/orgs/acme/hooks",
		"GET /api/v1/repos/acme/widget/hooks/7",
		"POST /api/v1/repos/acme/widget/hooks",
		"PATCH /api/v1/orgs/acme/hooks/7",
		"POST /api/v1/repos/acme/widget/hooks/7/tests",
		"DELETE /api/v1/orgs/acme/hooks/7",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		3: {"type": "forgejo", "active": true, "branch_filter": "main", "authorization_header": "Bearer x",
			"config": map[string]any{"url": "https://h/x", "content_type": "json", "secret": "s"},
			"events": []any{"push", "pull_request_review_approved", "pull_request_review_rejected", "pull_request_review_comment"}},
		4: {"active": false, "config": map[string]any{"secret": "new"}},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestWebhookDroppedEvents(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		// Forgejo answers 201 but keeps only the events it knows.
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":9,"config":{"url":"https://h/x","content_type":"json"},"events":["push"],"active":true}`)
	})
	hook, err := c.Webhooks().Create(context.Background(), gitoperationslib.RepoHooks("acme", "widget"), gitoperationslib.CreateWebhookOptions{
		URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, "no_such_event"},
	})
	if err == nil || !strings.Contains(err.Error(), "no_such_event") || hook == nil || hook.ID != 9 {
		t.Errorf("Create = %+v, %v; want the hook and an error naming the dropped event", hook, err)
	}
}

func TestWebhooksRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	hooks := c.Webhooks()
	if _, err := hooks.List(ctx, gitoperationslib.HookScope{}, gitoperationslib.ListOptions{}); err == nil {
		t.Error("empty scope accepted")
	}
	if _, err := hooks.Create(ctx, gitoperationslib.OrgHooks("acme"), gitoperationslib.CreateWebhookOptions{URL: "https://h/x"}); err == nil {
		t.Error("no events accepted")
	}
	if _, err := hooks.Edit(ctx, gitoperationslib.OrgHooks("acme"), 1, gitoperationslib.EditWebhookOptions{Events: []gitoperationslib.HookEvent{}}); err == nil {
		t.Error("empty event list accepted on edit")
	}
	if err := hooks.Ping(ctx, gitoperationslib.OrgHooks("acme"), 1); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("org ping: err = %v, want ErrNotSupported", err)
	}
}
