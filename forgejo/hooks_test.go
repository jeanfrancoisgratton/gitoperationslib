package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
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

// hookPages answers like /user/hooks and /admin/hooks: pages and X-Total-Count,
// no Link header.
func hookPages(w http.ResponseWriter, r *http.Request, ids ...int) {
	limit, page := 30, 1
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		page = v
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(ids)))
	var items []string
	for i := (page - 1) * limit; i < page*limit && i < len(ids); i++ {
		items = append(items, fmt.Sprintf(`{"id":%d,"config":{"url":"https://h/x"},"events":["push"],"active":true}`, ids[i]))
	}
	io.WriteString(w, "["+strings.Join(items, ",")+"]")
}

func TestUserWebhooks(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/user/hooks":
			hookPages(w, r, 1, 2, 3, 4, 5)
		default:
			io.WriteString(w, `{"id":7,"config":{"url":"https://h/x"},"events":["push"],"active":true}`)
		}
	})
	ctx := context.Background()
	if !c.Capabilities().Has(gitoperationslib.CapUserWebhooks) {
		t.Error("CapUserWebhooks not announced")
	}
	var h gitoperationslib.UserHooks = c
	hooks := h.UserWebhooks()

	for _, opts := range []gitoperationslib.ListOptions{{}, {PerPage: 1}, {PerPage: 2}, {PerPage: 5}, {PerPage: 100}} {
		var ids []int64
		for hook, err := range gitoperationslib.All(ctx, opts, hooks.List) {
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, hook.ID)
		}
		if !slices.Equal(ids, []int64{1, 2, 3, 4, 5}) {
			t.Errorf("All with %+v = %v", opts, ids)
		}
	}

	requests = nil
	if _, err := hooks.Get(ctx, 7); err != nil {
		t.Error(err)
	}
	if _, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}}); err != nil {
		t.Error(err)
	}
	if _, err := hooks.Edit(ctx, 7, gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(true)}); err != nil {
		t.Error(err)
	}
	if err := hooks.Delete(ctx, 7); err != nil {
		t.Error(err)
	}
	want := []string{"GET /api/v1/user/hooks/7", "POST /api/v1/user/hooks", "PATCH /api/v1/user/hooks/7", "DELETE /api/v1/user/hooks/7"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}
}

// In the fake, 1 to 3 are system hooks (3 on the second page of a listing), and
// 9 is a default hook.
func fakeSiteHooks(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var writes []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method+" "+r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/hooks":
			limit := r.URL.Query()
			limit.Set("limit", "2")
			r.URL.RawQuery = limit.Encode()
			hookPages(w, r, 1, 2, 3)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			io.WriteString(w, `{"id":3,"config":{"url":"https://h/x"},"events":["push"],"active":true}`)
		}
	})
	return c, &writes
}

func TestSystemWebhooksKind(t *testing.T) {
	c, writes := fakeSiteHooks(t)
	ctx := context.Background()
	if !c.Capabilities().Has(gitoperationslib.CapSystemWebhooks) {
		t.Error("CapSystemWebhooks not announced")
	}
	var h gitoperationslib.SiteHooks = c
	hooks := h.SystemWebhooks()
	edit := gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)}

	if _, err := hooks.Get(ctx, 9); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(default hook): err = %v, want 404", err)
	}
	if _, err := hooks.Edit(ctx, 9, edit); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Edit(default hook): err = %v, want 404", err)
	}
	if err := hooks.Delete(ctx, 9); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Delete(default hook): err = %v, want 404", err)
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", *writes)
	}

	// 3 is on the second page: the check must walk the whole list.
	if got, err := hooks.Get(ctx, 3); err != nil || got.ID != 3 {
		t.Errorf("Get(3) = %+v, %v", got, err)
	}
	if _, err := hooks.Edit(ctx, 3, edit); err != nil {
		t.Error(err)
	}
	if err := hooks.Delete(ctx, 3); err != nil {
		t.Error(err)
	}
	want := []string{"PATCH /api/v1/admin/hooks/3", "DELETE /api/v1/admin/hooks/3"}
	if !slices.Equal(*writes, want) {
		t.Errorf("writes = %v, want %v", *writes, want)
	}
}

func TestDefaultWebhooksKind(t *testing.T) {
	c, writes := fakeSiteHooks(t)
	ctx := context.Background()
	hooks := c.DefaultWebhooks()
	edit := gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)}

	// 3 is a system hook, on the second page of the listing.
	if _, err := hooks.Get(ctx, 3); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(system hook): err = %v, want 404", err)
	}
	if _, err := hooks.Edit(ctx, 3, edit); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Edit(system hook): err = %v, want 404", err)
	}
	if err := hooks.Delete(ctx, 3); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Delete(system hook): err = %v, want 404", err)
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", *writes)
	}

	if _, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}}); err != nil {
		t.Error(err)
	}
	if _, err := hooks.Get(ctx, 9); err != nil {
		t.Error(err)
	}
	if _, err := hooks.Edit(ctx, 9, edit); err != nil {
		t.Error(err)
	}
	if err := hooks.Delete(ctx, 9); err != nil {
		t.Error(err)
	}
	want := []string{"POST /api/v1/admin/hooks", "PATCH /api/v1/admin/hooks/9", "DELETE /api/v1/admin/hooks/9"}
	if !slices.Equal(*writes, want) {
		t.Errorf("writes = %v, want %v", *writes, want)
	}
}

// A non-admin cannot list the system hooks: the check fails with the 403 and
// nothing is changed.
func TestSiteHooksForbidden(t *testing.T) {
	var writes []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"forbidden"}`)
	})
	ctx := context.Background()
	if err := c.SystemWebhooks().Delete(ctx, 1); !gitoperationslib.IsForbidden(err) {
		t.Errorf("system: err = %v, want 403", err)
	}
	if err := c.DefaultWebhooks().Delete(ctx, 1); !gitoperationslib.IsForbidden(err) {
		t.Errorf("default: err = %v, want 403", err)
	}
	if len(writes) != 0 {
		t.Errorf("sent %v", writes)
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
