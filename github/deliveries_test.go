package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// As recorded by GitHub on 2026-11-05, for a target it could not reach.
const deliverySummary = `{"id":3852360482247671808,"guid":"ada9c560-d933-11f1-9e86-6a2bc7a8850b","delivered_at":"2026-11-05T16:12:21.361Z",
	"redelivery":false,"duration":0.25,"status":"failed to connect to host","status_code":502,"event":"issues","action":"closed","repository_id":1}`

func TestListHookDeliveries(t *testing.T) {
	for _, tt := range []struct {
		scope gitoperationslib.HookScope
		path  string
	}{
		{gitoperationslib.RepoHooks("acme", "widget"), "/api/v3/repos/acme/widget/hooks/7/deliveries"},
		{gitoperationslib.OrgHooks("acme"), "/api/v3/orgs/acme/hooks/7/deliveries"},
	} {
		c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != tt.path || r.URL.Query().Get("cursor") != "v1_1" || r.URL.Query().Get("per_page") != "2" {
				t.Errorf("unexpected %s", r.URL)
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?per_page=2&cursor=v1_2>; rel="next"`, r.Host, r.URL.Path))
			io.WriteString(w, `[`+deliverySummary+`]`)
		})
		page, err := c.ListHookDeliveries(context.Background(), tt.scope, 7, gitoperationslib.ListHookDeliveriesOptions{Cursor: "v1_1", PerPage: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.NextCursor != "v1_2" {
			t.Fatalf("page = %+v", page)
		}
		d := page.Items[0]
		if d.ID != 3852360482247671808 || d.GUID != "ada9c560-d933-11f1-9e86-6a2bc7a8850b" || d.Event != "issues" || d.Action != "closed" ||
			d.Status != "failed to connect to host" || d.StatusCode != 502 || d.Redelivery || d.Duration != 250*time.Millisecond || d.DeliveredAt.IsZero() {
			t.Errorf("delivery = %+v", d)
		}
		if d.RequestHeaders != nil || d.RequestPayload != nil {
			t.Errorf("a list has no request: %+v", d)
		}
	}
}

func TestGetHookDelivery(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/acme/widget/hooks/7/deliveries/9":
			io.WriteString(w, `{"id":9,"event":"ping","request":{"headers":{"X-GitHub-Event":"ping"},"payload":{"zen":"x"}},
				"response":{"headers":{"Server":"s"},"payload":"thanks"}}`)
		case "/api/v3/orgs/acme/hooks/7/deliveries/9":
			io.WriteString(w, `{"id":9,"event":"ping","request":{"headers":{},"payload":null},"response":{"headers":{},"payload":null}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	})
	ctx := context.Background()
	d, err := c.GetHookDelivery(ctx, gitoperationslib.RepoHooks("acme", "widget"), 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(d.RequestPayload, &payload); err != nil || payload["zen"] != "x" {
		t.Errorf("payload = %s, %v", d.RequestPayload, err)
	}
	if d.RequestHeaders["X-GitHub-Event"] != "ping" || d.ResponseHeaders["Server"] != "s" || d.ResponseBody != "thanks" {
		t.Errorf("delivery = %+v", d)
	}
	// The target's answer can be empty.
	if d, err = c.GetHookDelivery(ctx, gitoperationslib.OrgHooks("acme"), 7, 9); err != nil || d.ResponseBody != "" {
		t.Errorf("Get = %+v, %v", d, err)
	}
	if _, err := c.GetHookDelivery(ctx, gitoperationslib.RepoHooks("acme", "widget"), 7, 1); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(unknown) = %v", err)
	}
}

// GitHub answers HTTP 202, which is not an error.
func TestRedeliverHook(t *testing.T) {
	var got []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/v3/repos/acme/widget/hooks/7/deliveries/1/attempts" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{}`)
	})
	ctx := context.Background()
	if err := c.RedeliverHook(ctx, gitoperationslib.RepoHooks("acme", "widget"), 7, 9); err != nil {
		t.Error(err)
	}
	if err := c.RedeliverHook(ctx, gitoperationslib.OrgHooks("acme"), 7, 9); err != nil {
		t.Error(err)
	}
	if err := c.RedeliverHook(ctx, gitoperationslib.RepoHooks("acme", "widget"), 7, 1); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Redeliver(unknown) = %v", err)
	}
	if got[0] != "POST /api/v3/repos/acme/widget/hooks/7/deliveries/9/attempts" || got[1] != "POST /api/v3/orgs/acme/hooks/7/deliveries/9/attempts" {
		t.Errorf("requests = %q", got)
	}
}

func TestDeliveriesNeedAScope(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("request sent: %s", r.URL) })
	ctx := context.Background()
	if _, err := c.ListHookDeliveries(ctx, gitoperationslib.HookScope{}, 7, gitoperationslib.ListHookDeliveriesOptions{}); err == nil {
		t.Error("List without a scope succeeded")
	}
	if _, err := c.GetHookDelivery(ctx, gitoperationslib.HookScope{}, 7, 9); err == nil {
		t.Error("Get without a scope succeeded")
	}
	if err := c.RedeliverHook(ctx, gitoperationslib.HookScope{}, 7, 9); err == nil {
		t.Error("Redeliver without a scope succeeded")
	}
}

// GitHub resets what a config leaves out, so an edit must send the current value.
func TestWebhookInsecureSSLGitHub(t *testing.T) {
	var config map[string]any
	insecure := "1"
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Config map[string]any `json:"config"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodGet {
			config = body.Config
			if v, ok := body.Config["insecure_ssl"].(string); ok {
				insecure = v
			}
		}
		fmt.Fprintf(w, `{"id":7,"active":true,"events":["push"],"config":{"url":"https://h/x","content_type":"json","insecure_ssl":%q}}`, insecure)
	})
	ctx := context.Background()
	scope := gitoperationslib.RepoHooks("acme", "widget")
	hook, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{URL: "https://h/x", Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}, InsecureSSL: true})
	if err != nil || !hook.InsecureSSL || config["insecure_ssl"] != "1" {
		t.Errorf("Create = %+v, %v; sent %v", hook, err, config)
	}
	url := "https://h/y"
	if hook, err = c.Webhooks().Edit(ctx, scope, 7, gitoperationslib.EditWebhookOptions{URL: &url}); err != nil || !hook.InsecureSSL || config["insecure_ssl"] != "1" {
		t.Errorf("Edit(URL) = %+v, %v; sent %v", hook, err, config)
	}
	if hook, err = c.Webhooks().Edit(ctx, scope, 7, gitoperationslib.EditWebhookOptions{InsecureSSL: gitoperationslib.Ptr(false)}); err != nil || hook.InsecureSSL || config["insecure_ssl"] != "0" || config["url"] != "https://h/x" {
		t.Errorf("Edit(false) = %+v, %v; sent %v", hook, err, config)
	}
}
