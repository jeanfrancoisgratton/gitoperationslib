package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const gitHooksBase = "/api/v1/repos/acme/widget/hooks/git"

func TestGitHooks(t *testing.T) {
	var requests []string
	var patched map[string]string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET " + gitHooksBase:
			io.WriteString(w, `[{"name":"pre-receive","is_active":true,"content":"#!/bin/sh\n"},{"name":"update","is_active":false}]`)
		case "GET " + gitHooksBase + "/update":
			io.WriteString(w, `{"name":"update","is_active":false}`)
		case "PATCH " + gitHooksBase + "/update":
			json.NewDecoder(r.Body).Decode(&patched)
			io.WriteString(w, `{"name":"update","is_active":true,"content":"#!/bin/sh\nexit 0\n"}`)
		case "DELETE " + gitHooksBase + "/update":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"The target couldn't be found."}`)
		}
	})
	ctx := context.Background()
	list, err := c.GitHooks().List(ctx, "acme", "widget")
	want := []gitoperationslib.GitHook{{Name: "pre-receive", Active: true, Content: "#!/bin/sh\n"}, {Name: "update"}}
	if err != nil || !slices.Equal(list, want) {
		t.Errorf("List = %+v, %v", list, err)
	}
	got, err := c.GitHooks().Get(ctx, "acme", "widget", "update")
	if err != nil || got.Active || got.Name != "update" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := c.GitHooks().Get(ctx, "acme", "widget", "nosuch"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(nosuch) = %v", err)
	}
	const script = "#!/bin/sh\nexit 0\n"
	got, err = c.GitHooks().Set(ctx, "acme", "widget", "update", script)
	if err != nil || !got.Active || got.Content != script || patched["content"] != script {
		t.Errorf("Set = %+v, %v; sent %v", got, err, patched)
	}
	if err := c.GitHooks().Delete(ctx, "acme", "widget", "update"); err != nil {
		t.Error(err)
	}
}

// The server takes empty content for a request to delete the hook.
func TestGitHooksRefusedWithoutRequest(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("request sent: %s", r.URL) })
	ctx := context.Background()
	if _, err := c.GitHooks().Set(ctx, "acme", "widget", "update", ""); err == nil {
		t.Error("Set without content succeeded")
	}
	if _, err := c.GitHooks().Set(ctx, "acme", "widget", "", "x"); err == nil {
		t.Error("Set without a name succeeded")
	}
	if _, err := c.GitHooks().Get(ctx, "acme", "widget", ""); err == nil {
		t.Error("Get without a name succeeded")
	}
	if err := c.GitHooks().Delete(ctx, "acme", "widget", ""); err == nil {
		t.Error("Delete without a name succeeded")
	}
}

func TestGitHooksDisabledOnServer(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"must be allowed to edit Git hooks"}`)
	})
	_, err := c.GitHooks().List(context.Background(), "acme", "widget")
	var api *gitoperationslib.APIError
	if !errors.As(err, &api) || api.StatusCode != http.StatusForbidden || api.Message != "must be allowed to edit Git hooks" {
		t.Errorf("err = %v", err)
	}
}
