package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const hookResponse = `{"id":7,"name":"web","active":true,"events":["push","gollum"],
	"config":{"url":"https://h/x","content_type":"json","secret":"********"},
	"created_at":"2026-10-17T10:00:00Z","updated_at":"2026-10-17T11:00:00Z"}`

func TestWebhooksRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete, strings.HasSuffix(r.URL.Path, "/pings"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			io.WriteString(w, "["+hookResponse+"]")
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, hookResponse)
		default:
			io.WriteString(w, hookResponse)
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
	if len(list.Items) != 1 || fmt.Sprint(list.Items[0].Events) != "[push wiki]" || list.Items[0].CreatedAt.Hour() != 10 {
		t.Errorf("List = %+v (gollum must read back as wiki)", list)
	}
	_, err = hooks.List(ctx, org, gitoperationslib.ListOptions{})
	check(err)
	_, err = hooks.Create(ctx, repo, gitoperationslib.CreateWebhookOptions{
		URL: "https://h/x", Secret: "s", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush, gitoperationslib.HookEventWiki},
	})
	check(err)
	_, err = hooks.Create(ctx, org, gitoperationslib.CreateWebhookOptions{URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}, Active: gitoperationslib.Ptr(false)})
	check(err)
	// Changing the URL fetches the current config first: GitHub replaces it whole.
	_, err = hooks.Edit(ctx, repo, 7, gitoperationslib.EditWebhookOptions{URL: gitoperationslib.Ptr("https://h/y"), Secret: gitoperationslib.Ptr("s2")})
	check(err)
	_, err = hooks.Edit(ctx, org, 7, gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)})
	check(err)
	check(hooks.Ping(ctx, repo, 7))
	check(hooks.Ping(ctx, org, 7))
	check(hooks.Delete(ctx, repo, 7))

	wantRequests := []string{
		"GET /api/v3/repos/acme/widget/hooks",
		"GET /api/v3/orgs/acme/hooks",
		"POST /api/v3/repos/acme/widget/hooks",
		"POST /api/v3/orgs/acme/hooks",
		"GET /api/v3/repos/acme/widget/hooks/7",
		"PATCH /api/v3/repos/acme/widget/hooks/7",
		"PATCH /api/v3/orgs/acme/hooks/7",
		"POST /api/v3/repos/acme/widget/hooks/7/pings",
		"POST /api/v3/orgs/acme/hooks/7/pings",
		"DELETE /api/v3/repos/acme/widget/hooks/7",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		2: {"name": "web", "active": true, "events": []any{"push", "gollum"},
			"config": map[string]any{"url": "https://h/x", "content_type": "json", "secret": "s"}},
		3: {"name": "web", "active": false, "events": []any{"push"}, "config": map[string]any{"url": "https://h/x", "content_type": "json"}},
		5: {"config": map[string]any{"url": "https://h/y", "content_type": "json", "secret": "s2"}},
		6: {"active": false},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestWebhooksRejected(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	hooks := c.Webhooks()
	scope := gitoperationslib.RepoHooks("acme", "widget")
	push := []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}
	for name, opts := range map[string]gitoperationslib.CreateWebhookOptions{
		"branch filter": {URL: "https://h/x", Events: push, BranchFilter: "main"},
		"auth header":   {URL: "https://h/x", Events: push, AuthorizationHeader: "Bearer x"},
	} {
		if _, err := hooks.Create(ctx, scope, opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if _, err := hooks.Edit(ctx, scope, 1, gitoperationslib.EditWebhookOptions{BranchFilter: gitoperationslib.Ptr("main")}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("edit branch filter: err = %v", err)
	}
	if _, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{Events: push}); err == nil {
		t.Error("missing URL accepted")
	}
	if err := hooks.Delete(ctx, gitoperationslib.HookScope{}, 1); err == nil {
		t.Error("empty scope accepted")
	}
}
