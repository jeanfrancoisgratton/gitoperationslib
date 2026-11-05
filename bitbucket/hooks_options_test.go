package bitbucket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Bitbucket resets skip_cert_verification when a PUT omits it.
func TestWebhookInsecureSSLBitbucket(t *testing.T) {
	var sent []map[string]any
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"values":[{"uuid":"{6f1c2a9e-1111-2222-3333-444455556666}","url":"https://h/x","active":true,"events":["repo:push"],"skip_cert_verification":true}]}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body)
		json.NewEncoder(w).Encode(map[string]any{"uuid": "{6f1c2a9e-1111-2222-3333-444455556666}", "url": body["url"], "active": true, "events": body["events"], "skip_cert_verification": body["skip_cert_verification"]})
	})
	ctx := context.Background()
	scope := gitoperationslib.RepoHooks("acme", "widget")
	hook, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}, InsecureSSL: true})
	if err != nil || !hook.InsecureSSL {
		t.Fatalf("Create = %+v, %v", hook, err)
	}
	got, err := c.Webhooks().Get(ctx, scope, hook.ID)
	if err != nil || !got.InsecureSSL {
		t.Errorf("Get = %+v, %v", got, err)
	}
	url := "https://h/y"
	if hook, err = c.Webhooks().Edit(ctx, scope, hook.ID, gitoperationslib.EditWebhookOptions{URL: &url}); err != nil || !hook.InsecureSSL {
		t.Errorf("Edit(URL) = %+v, %v", hook, err)
	}
	if hook, err = c.Webhooks().Edit(ctx, scope, hook.ID, gitoperationslib.EditWebhookOptions{InsecureSSL: gitoperationslib.Ptr(false)}); err != nil || hook.InsecureSSL {
		t.Errorf("Edit(false) = %+v, %v", hook, err)
	}
	if len(sent) != 3 {
		t.Errorf("%d writes", len(sent))
	}
}

func TestWebhookWildcardBitbucket(t *testing.T) {
	keys, err := toEventKeys([]gitoperationslib.HookEvent{gitoperationslib.HookEventAll}, "webhooks.create")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"repo:push", "pullrequest:created", "pullrequest:approved", "pullrequest:comment_created"} {
		if !slices.Contains(keys, want) {
			t.Errorf("lacks %s: %v", want, keys)
		}
	}
	// The issue tracker is retired.
	if slices.ContainsFunc(keys, func(k string) bool { return len(k) > 6 && k[:6] == "issue:" }) {
		t.Errorf("issue events in %v", keys)
	}
	// Read back, they are not reported as the wildcard.
	if slices.Contains(fromEventKeys(keys), gitoperationslib.HookEventAll) {
		t.Errorf("folded into a wildcard: %v", fromEventKeys(keys))
	}
}
