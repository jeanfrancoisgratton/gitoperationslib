package forgejo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const pushMirrorsBase = "/api/v1/repos/acme/widget/push_mirrors"

// Responses as sent by 15.0.11, the empty message included.
const (
	neverSynced = `{"repo_name":"widget","remote_name":"remote_mirror_a","remote_address":"https://example.com/x/y.git","created":"2026-11-05T13:40:27Z","last_update":"1970-01-01T00:00:00Z","last_error":"","interval":"8h0m0s","sync_on_commit":true,"public_key":"","branch_filter":"main"}`
	synced      = `{"remote_name":"remote_mirror_b","remote_address":"ssh://example.com/x/y.git","created":"2026-11-05T13:40:27Z","last_update":"2026-11-05T14:00:00Z","last_error":"exit status 128","interval":"0s","public_key":"ssh-ed25519 AAAA"}`
	nilMessage  = `{"message":"%!s(<nil>)","url":"http://x/api/swagger"}`
)

func TestPushMirrorRead(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pushMirrorsBase:
			if r.URL.Query().Get("page") != "1" {
				t.Errorf("page = %q: without one, the server ignores the limit", r.URL.Query().Get("page"))
			}
			w.Header().Set("X-Total-Count", "3")
			io.WriteString(w, `[`+neverSynced+`,`+synced+`]`)
		case pushMirrorsBase + "/remote_mirror_b":
			io.WriteString(w, synced)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, nilMessage)
		}
	})
	ctx := context.Background()
	page, err := c.ListPushMirrors(ctx, "acme", "widget", gitoperationslib.ListOptions{PerPage: 2}) // no page asked for
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextPage != 2 {
		t.Fatalf("page = %+v", page)
	}
	first := page.Items[0]
	if first.Name != "remote_mirror_a" || first.RemoteAddress != "https://example.com/x/y.git" || first.Interval != 8*time.Hour ||
		!first.SyncOnCommit || first.BranchFilter != "main" || first.CreatedAt.IsZero() {
		t.Errorf("first = %+v", first)
	}
	if !first.LastSync.IsZero() {
		t.Errorf("LastSync of a mirror never pushed = %v", first.LastSync)
	}

	got, err := c.GetPushMirror(ctx, "acme", "widget", "remote_mirror_b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != 0 || got.PublicKey != "ssh-ed25519 AAAA" || got.LastError != "exit status 128" ||
		!got.LastSync.Equal(time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("got = %+v", got)
	}

	_, err = c.GetPushMirror(ctx, "acme", "widget", "gone")
	if !gitoperationslib.IsNotFound(err) || !describedGone(err) {
		t.Errorf("Get(gone) = %v", err)
	}
	if _, err := c.GetPushMirror(ctx, "acme", "widget", ""); err == nil {
		t.Error("Get without a name succeeded")
	}
}

// describedGone reports whether err gives a real message instead of the
// server's.
func describedGone(err error) bool {
	api, ok := err.(*gitoperationslib.APIError)
	return ok && api.Message == `push mirror "gone" not found`
}

func TestCreatePushMirror(t *testing.T) {
	var got map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != pushMirrorsBase {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, neverSynced)
	})
	ctx := context.Background()
	m, err := c.CreatePushMirror(ctx, "acme", "widget", gitoperationslib.CreatePushMirrorOptions{
		RemoteAddress: "https://example.com/x/y.git", Username: "u", Password: "p",
		Interval: 8 * time.Hour, SyncOnCommit: true, BranchFilter: "main",
	})
	if err != nil || m.Name != "remote_mirror_a" {
		t.Fatalf("Create = %+v, %v", m, err)
	}
	want := map[string]any{
		"remote_address": "https://example.com/x/y.git", "remote_username": "u", "remote_password": "p",
		"interval": "8h0m0s", "sync_on_commit": true, "branch_filter": "main",
	}
	if len(got) != len(want) {
		t.Errorf("body = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, got[k], v)
		}
	}

	// Without an interval the server would refuse the request: the field is
	// always sent.
	if _, err := c.CreatePushMirror(ctx, "acme", "widget", gitoperationslib.CreatePushMirrorOptions{RemoteAddress: "git@example.com:x/y.git", UseSSH: true}); err != nil {
		t.Fatal(err)
	}
	if got["interval"] != "0s" || got["use_ssh"] != true {
		t.Errorf("body = %v", got)
	}
}

func TestCreatePushMirrorRefused(t *testing.T) {
	status, body := 0, ""
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
	opts := gitoperationslib.CreatePushMirrorOptions{RemoteAddress: "https://example.invalid/x.git"}
	tests := []struct {
		name, body  string
		status      int
		wantStatus  int
		wantMessage string
	}{
		{"interval below the minimum", nilMessage, 400, 400, "the interval is below the server's minimum"},
		{"remote refused", `{"message":"Permission denied"}`, 401, 400, `the server refuses the remote address "https://example.invalid/x.git"`},
		{"plain http on Forgejo 17", `{"message":"Unknown error"}`, 400, 400, `the server refuses the remote address "https://example.invalid/x.git": since Forgejo 17 that includes plain http`},
		{"really unauthorized", `{"message":"token is required"}`, 401, 401, "token is required"},
		{"other", `{"message":"The URL contains credentials"}`, 400, 400, "The URL contains credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body = tt.status, tt.body
			_, err := c.CreatePushMirror(context.Background(), "acme", "widget", opts)
			api, ok := err.(*gitoperationslib.APIError)
			if !ok || api.StatusCode != tt.wantStatus || api.Message != tt.wantMessage {
				t.Errorf("err = %#v", err)
			}
		})
	}
}

func TestCreatePushMirrorValidates(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("request sent: %s", r.URL) })
	for _, opts := range []gitoperationslib.CreatePushMirrorOptions{
		{},
		{RemoteAddress: "x", UseSSH: true, Username: "u"},
		{RemoteAddress: "x", UseSSH: true, Password: "p"},
		{RemoteAddress: "x", Interval: -time.Hour},
	} {
		if _, err := c.CreatePushMirror(context.Background(), "acme", "widget", opts); err == nil {
			t.Errorf("Create(%+v) succeeded", opts)
		}
	}
}

func TestDeleteAndSyncMirrors(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.URL.Path == pushMirrorsBase+"/gone" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, nilMessage)
		}
	})
	ctx := context.Background()
	if err := c.DeletePushMirror(ctx, "acme", "widget", "m"); err != nil {
		t.Error(err)
	}
	if err := c.DeletePushMirror(ctx, "acme", "widget", "gone"); !gitoperationslib.IsNotFound(err) || !describedGone(err) {
		t.Errorf("Delete(gone) = %v", err)
	}
	if err := c.DeletePushMirror(ctx, "acme", "widget", ""); err == nil {
		t.Error("Delete without a name succeeded")
	}
	if err := c.SyncPushMirrors(ctx, "acme", "widget"); err != nil {
		t.Error(err)
	}
	if err := c.SyncMirror(ctx, "acme", "widget"); err != nil {
		t.Error(err)
	}
	want := []string{
		"DELETE " + pushMirrorsBase + "/m", "DELETE " + pushMirrorsBase + "/gone",
		"POST /api/v1/repos/acme/widget/push_mirrors-sync", "POST /api/v1/repos/acme/widget/mirror-sync",
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %q", requests)
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, requests[i], want[i])
		}
	}
}
