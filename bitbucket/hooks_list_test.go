package bitbucket

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func newServer(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestListWebhooksBitbucket(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		// Bitbucket pages webhooks with a token, and ignores page=.
		if r.URL.Query().Get("next") == "" && !strings.Contains(r.URL.Path, "workspaces") {
			io.WriteString(w, `{"values":[{"uuid":"{6f1c2a9e-1111-2222-3333-444455556666}","url":"https://h/x","active":true,"events":["repo:push"]}],"pagelen":100,"next":"https://x/?next=0d6cd10f&pagelen=100"}`)
			return
		}
		io.WriteString(w, `{"values":[{"uuid":"{7f1c2a9e-1111-2222-3333-444455556666}","url":"https://h/y","active":true,"events":["repo:push"]}],"pagelen":100}`)
	})
	ctx := context.Background()
	page, err := c.Webhooks().List(ctx, gitoperationslib.RepoHooks("acme", "widget"), gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 2 || page.NextPage != 0 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if h := page.Items[0]; h.URL != "https://h/x" || !h.Active || h.ID == 0 || !slices.Equal(h.Events, []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}) {
		t.Errorf("hook = %+v", h)
	}
	if _, err := c.Webhooks().List(ctx, gitoperationslib.OrgHooks("acme"), gitoperationslib.ListOptions{}); err != nil {
		t.Error(err)
	}
	if !slices.Equal(requests, []string{"/2.0/repositories/acme/widget/hooks", "/2.0/repositories/acme/widget/hooks", "/2.0/workspaces/acme/hooks"}) {
		t.Errorf("requests = %v", requests)
	}
	n := len(requests)
	if _, err := c.Webhooks().List(ctx, gitoperationslib.HookScope{}, gitoperationslib.ListOptions{}); err == nil {
		t.Error("empty scope accepted")
	}
	if len(requests) != n {
		t.Errorf("empty scope sent %v", requests[n:])
	}
}

// Deleting a secret must not reach a variable of the same name, and the reverse.
func TestSecretDeleteBitbucket(t *testing.T) {
	var deleted []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		io.WriteString(w, `{"values":[{"uuid":"{v1}","key":"ENV","value":"prod","secured":false},{"uuid":"{s1}","key":"TOKEN","secured":true}]}`)
	})
	ctx := context.Background()
	repo := gitoperationslib.RepoCI("acme", "widget")
	if err := c.Secrets().Delete(ctx, repo, "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(deleted, []string{"/2.0/repositories/acme/widget/pipelines_config/variables/{s1}"}) {
		t.Errorf("deleted = %v", deleted)
	}
	deleted = nil
	if err := c.Secrets().Delete(ctx, repo, "ENV"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("a variable as a secret: err = %v, want 404", err)
	}
	if err := c.Variables().Delete(ctx, repo, "TOKEN"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("a secret as a variable: err = %v, want 404", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted = %v, want nothing", deleted)
	}
}
