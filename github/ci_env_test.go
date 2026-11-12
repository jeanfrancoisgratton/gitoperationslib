package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestEnvironmentSecretsGitHub(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const base = "/repos/acme/widget/environments/Prod%20EU"
	var requests []string
	var stored map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v3")
		requests = append(requests, r.Method+" "+path)
		switch r.Method + " " + path {
		case "GET " + base + "/secrets/public-key":
			io.WriteString(w, `{"key_id": "k1", "key": "`+base64.StdEncoding.EncodeToString(pub[:])+`"}`)
		case "PUT " + base + "/secrets/TOKEN":
			json.NewDecoder(r.Body).Decode(&stored)
			w.WriteHeader(http.StatusCreated)
		case "GET " + base + "/secrets":
			io.WriteString(w, `{"total_count": 1, "secrets": [{"name": "TOKEN", "created_at": "2026-10-30T12:00:00Z", "updated_at": "2026-10-30T13:00:00Z"}]}`)
		case "DELETE " + base + "/secrets/TOKEN":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
		}
	})
	ctx := context.Background()
	scope := gitoperationslib.EnvCI("acme", "widget", "Prod EU")
	if err := c.CI().Secrets().Set(ctx, scope, "TOKEN", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.StdEncoding.DecodeString(stored["encrypted_value"].(string))
	if plain, ok := box.OpenAnonymous(nil, sealed, pub, priv); !ok || string(plain) != "s3cr3t" || stored["key_id"] != "k1" {
		t.Errorf("stored = %v, decrypts to %q (%v)", stored, plain, ok)
	}
	if _, ok := stored["visibility"]; ok {
		t.Errorf("an environment secret has no visibility: %v", stored)
	}
	page, err := c.CI().Secrets().List(ctx, scope, gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "TOKEN" {
		t.Errorf("List = %+v, %v", page, err)
	}
	if err := c.CI().Secrets().Delete(ctx, scope, "TOKEN"); err != nil {
		t.Error(err)
	}
	if len(requests) != 4 {
		t.Errorf("requests = %q", requests)
	}
}

func TestEnvironmentVariablesGitHub(t *testing.T) {
	const base = "/repos/acme/widget/environments/prod/variables"
	var requests []string
	exists := false
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v3")
		requests = append(requests, r.Method+" "+path)
		switch r.Method + " " + path {
		case "PATCH " + base + "/ENV":
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"message": "Not Found"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "POST " + base:
			exists = true
			w.WriteHeader(http.StatusCreated)
		case "GET " + base + "/ENV":
			io.WriteString(w, `{"name": "ENV", "value": "v1"}`)
		case "GET " + base:
			io.WriteString(w, `{"total_count": 1, "variables": [{"name": "ENV", "value": "v1"}]}`)
		case "DELETE " + base + "/ENV":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
		}
	})
	ctx := context.Background()
	scope := gitoperationslib.EnvCI("acme", "widget", "prod")
	for range 2 { // creates, then updates
		if err := c.CI().Variables().Set(ctx, scope, "ENV", "v1"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"PATCH " + base + "/ENV", "POST " + base, "PATCH " + base + "/ENV"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %q, want %q", requests, want)
	}
	if v, err := c.CI().Variables().Get(ctx, scope, "ENV"); err != nil || v.Value != "v1" {
		t.Errorf("Get = %+v, %v", v, err)
	}
	if page, err := c.CI().Variables().List(ctx, scope, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("List = %+v, %v", page, err)
	}
	if err := c.CI().Variables().Delete(ctx, scope, "ENV"); err != nil {
		t.Error(err)
	}
}

func TestEnvironmentScopeNeedsARepositoryGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("request sent: %s", r.URL) })
	for _, scope := range []gitoperationslib.CIScope{{Owner: "acme", Environment: "prod"}, {Environment: "prod"}} {
		if err := c.CI().Secrets().Set(context.Background(), scope, "X", "x"); err == nil {
			t.Errorf("Set(%+v) succeeded", scope)
		}
	}
}

// orgSecretServer is an organisation whose secret TOKEN has the given access.
func orgSecretServer(t *testing.T, visibility string) (*Client, *map[string]any, *[]string) {
	t.Helper()
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const base = "/api/v3/orgs/acme/actions/secrets"
	stored := map[string]any{}
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET " + base + "/public-key":
			io.WriteString(w, `{"key_id": "k1", "key": "`+base64.StdEncoding.EncodeToString(pub[:])+`"}`)
		case "GET " + base + "/TOKEN":
			if visibility == "" {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"message": "Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"name": "TOKEN", "visibility": %q}`, visibility)
		case "PUT " + base + "/TOKEN":
			clear(stored)
			json.NewDecoder(r.Body).Decode(&stored)
			w.WriteHeader(http.StatusCreated)
		case "GET " + base + "/TOKEN/repositories":
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
				io.WriteString(w, `{"total_count": 2, "repositories": [{"id": 11, "name": "one"}]}`)
				return
			}
			io.WriteString(w, `{"total_count": 2, "repositories": [{"id": 22, "name": "two"}]}`)
		case "PUT " + base + "/TOKEN/repositories":
			clear(stored)
			json.NewDecoder(r.Body).Decode(&stored)
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/v3/repos/acme/one":
			io.WriteString(w, `{"id": 11, "name": "one"}`)
		case "GET /api/v3/repos/acme/two":
			io.WriteString(w, `{"id": 22, "name": "two"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message": "Not Found"}`)
		}
	})
	return c, &stored, &requests
}

func ids(v any) string { return fmt.Sprint(v) }

// A new value must not widen who can use the secret: GitHub takes the visibility
// from each request, so Set has to send what is there.
func TestSetKeepsOrgSecretAccess(t *testing.T) {
	tests := []struct {
		name, current, wantVisibility, wantIDs string
	}{
		{"new secret", "", "all", "<nil>"},
		{"available to all", "all", "all", "<nil>"},
		{"private repositories", "private", "private", "<nil>"},
		{"selected repositories", "selected", "selected", "[11 22]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, stored, _ := orgSecretServer(t, tt.current)
			if err := c.CI().Secrets().Set(context.Background(), gitoperationslib.OrgCI("acme"), "TOKEN", "new value"); err != nil {
				t.Fatal(err)
			}
			if got := (*stored)["visibility"]; got != tt.wantVisibility {
				t.Errorf("visibility = %v, want %s", got, tt.wantVisibility)
			}
			if got := ids((*stored)["selected_repository_ids"]); got != tt.wantIDs {
				t.Errorf("repositories = %s, want %s", got, tt.wantIDs)
			}
		})
	}
}

func TestGetOrgSecretAccess(t *testing.T) {
	ctx := context.Background()
	c, _, _ := orgSecretServer(t, "selected")
	got, err := c.CI().Secrets().GetAccess(ctx, "acme", "TOKEN")
	if err != nil || got.Visibility != gitoperationslib.SecretVisibleSelected || !slices.Equal(got.Repos, []string{"one", "two"}) {
		t.Errorf("selected = %+v, %v", got, err)
	}
	c, _, requests := orgSecretServer(t, "private")
	got, err = c.CI().Secrets().GetAccess(ctx, "acme", "TOKEN")
	if err != nil || got.Visibility != gitoperationslib.SecretVisiblePrivate || got.Repos != nil || len(*requests) != 1 {
		t.Errorf("private = %+v, %v after %q", got, err, *requests)
	}
	c, _, _ = orgSecretServer(t, "")
	if _, err := c.CI().Secrets().GetAccess(ctx, "acme", "TOKEN"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown secret: err = %v", err)
	}
	if _, err := c.CI().Secrets().GetAccess(ctx, "acme", ""); err == nil {
		t.Error("no name accepted")
	}
}

func TestSetOrgSecret(t *testing.T) {
	ctx := context.Background()
	c, stored, _ := orgSecretServer(t, "")
	selected := gitoperationslib.SecretAccess{Visibility: gitoperationslib.SecretVisibleSelected, Repos: []string{"one", "two"}}
	if err := c.CI().Secrets().SetWithAccess(ctx, "acme", "TOKEN", "v", selected); err != nil {
		t.Fatal(err)
	}
	if (*stored)["visibility"] != "selected" || ids((*stored)["selected_repository_ids"]) != "[11 22]" || (*stored)["encrypted_value"] == nil {
		t.Errorf("stored = %v", *stored)
	}
	if err := c.CI().Secrets().SetWithAccess(ctx, "acme", "TOKEN", "v", gitoperationslib.SecretAccess{Visibility: gitoperationslib.SecretVisibleAll}); err != nil {
		t.Fatal(err)
	}
	if (*stored)["visibility"] != "all" || (*stored)["selected_repository_ids"] != nil {
		t.Errorf("stored = %v", *stored)
	}
	if err := c.CI().Secrets().SetWithAccess(ctx, "acme", "TOKEN", "v", gitoperationslib.SecretAccess{Visibility: gitoperationslib.SecretVisibleSelected, Repos: []string{"gone"}}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown repository: err = %v", err)
	}
}

func TestSetOrgSecretRefused(t *testing.T) {
	c, _, requests := orgSecretServer(t, "")
	ctx := context.Background()
	for _, access := range []gitoperationslib.SecretAccess{
		{},
		{Visibility: "everyone"},
		{Visibility: gitoperationslib.SecretVisibleAll, Repos: []string{"one"}},
		{Visibility: gitoperationslib.SecretVisiblePrivate, Repos: []string{"one"}},
	} {
		if err := c.CI().Secrets().SetWithAccess(ctx, "acme", "TOKEN", "v", access); err == nil {
			t.Errorf("SetOrgSecret(%+v) succeeded", access)
		}
	}
	if err := c.CI().Secrets().SetWithAccess(ctx, "", "TOKEN", "v", gitoperationslib.SecretAccess{Visibility: gitoperationslib.SecretVisibleAll}); err == nil {
		t.Error("no organisation accepted")
	}
	if len(*requests) != 0 {
		t.Errorf("requests sent: %q", *requests)
	}
}

func TestSetOrgSecretRepos(t *testing.T) {
	ctx := context.Background()
	c, stored, _ := orgSecretServer(t, "selected")
	if err := c.CI().Secrets().SetRepos(ctx, "acme", "TOKEN", []string{"two"}); err != nil {
		t.Fatal(err)
	}
	if ids((*stored)["selected_repository_ids"]) != "[22]" {
		t.Errorf("stored = %v", *stored)
	}
	// No repositories: nobody can use it, which must reach the server.
	if err := c.CI().Secrets().SetRepos(ctx, "acme", "TOKEN", nil); err != nil {
		t.Fatal(err)
	}
	if ids((*stored)["selected_repository_ids"]) != "[]" {
		t.Errorf("stored = %v", *stored)
	}
	if err := c.CI().Secrets().SetRepos(ctx, "acme", "TOKEN", []string{"gone"}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown repository: err = %v", err)
	}
	if err := c.CI().Secrets().SetRepos(ctx, "", "TOKEN", nil); err == nil {
		t.Error("no organisation accepted")
	}
}
