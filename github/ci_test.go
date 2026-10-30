package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestSecretsGitHub(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/repos/acme/widget/actions/secrets/public-key", "GET /api/v3/orgs/acme/actions/secrets/public-key":
			io.WriteString(w, `{"key_id": "k1", "key": "`+base64.StdEncoding.EncodeToString(pub[:])+`"}`)
		case "PUT /api/v3/repos/acme/widget/actions/secrets/TOKEN", "PUT /api/v3/orgs/acme/actions/secrets/TOKEN":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if stored == nil {
				stored = map[string]map[string]any{}
			}
			stored[r.URL.Path] = body
			w.WriteHeader(http.StatusCreated)
		case "GET /api/v3/repos/acme/widget/actions/secrets":
			io.WriteString(w, `{"total_count": 1, "secrets": [{"name": "TOKEN", "created_at": "2026-10-30T12:00:00Z", "updated_at": "2026-10-30T13:00:00Z"}]}`)
		case "DELETE /api/v3/orgs/acme/actions/secrets/TOKEN":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	if err := c.Secrets().Set(ctx, gitoperationslib.RepoCI("acme", "widget"), "TOKEN", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	if err := c.Secrets().Set(ctx, gitoperationslib.OrgCI("acme"), "TOKEN", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	for path, body := range stored {
		sealed, _ := base64.StdEncoding.DecodeString(body["encrypted_value"].(string))
		plain, ok := box.OpenAnonymous(nil, sealed, pub, priv)
		if !ok || string(plain) != "s3cr3t" || body["key_id"] != "k1" {
			t.Errorf("%s: body %v decrypts to %q (%v)", path, body, plain, ok)
		}
	}
	if v := stored["/api/v3/orgs/acme/actions/secrets/TOKEN"]["visibility"]; v != "all" {
		t.Errorf("org secret visibility = %v, want all", v)
	}
	page, err := c.Secrets().List(ctx, gitoperationslib.RepoCI("acme", "widget"), gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "TOKEN" || page.Items[0].UpdatedAt.IsZero() {
		t.Errorf("List = %+v, %v", page, err)
	}
	if err := c.Secrets().Delete(ctx, gitoperationslib.OrgCI("acme"), "TOKEN"); err != nil {
		t.Error(err)
	}
	if err := c.Secrets().Set(ctx, gitoperationslib.UserCI(), "X", "x"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user scope: err = %v, want ErrNotSupported", err)
	}
	if _, err := seal("not base64!", "x"); err == nil {
		t.Error("bad public key accepted")
	}
}

func TestVariablesGitHub(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	existing := map[string]bool{}
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodPatch && !existing[r.URL.Path]:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message": "Not Found"}`)
		case r.Method == http.MethodPatch, r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost:
			existing[r.URL.Path+"/"+body["name"].(string)] = true
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/api/v3/repos/acme/widget/actions/variables/ENV":
			io.WriteString(w, `{"name": "ENV", "value": "prod", "created_at": "2026-10-30T12:00:00Z"}`)
		case r.URL.Path == "/api/v3/orgs/acme/actions/variables":
			io.WriteString(w, `{"total_count": 1, "variables": [{"name": "ENV", "value": "prod"}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	repo, org := gitoperationslib.RepoCI("acme", "widget"), gitoperationslib.OrgCI("acme")

	// Unknown: PATCH 404, then POST. Known: PATCH only.
	if err := c.Variables().Set(ctx, org, "ENV", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := c.Variables().Set(ctx, org, "ENV", "staging"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATCH /api/v3/orgs/acme/actions/variables/ENV",
		"POST /api/v3/orgs/acme/actions/variables",
		"PATCH /api/v3/orgs/acme/actions/variables/ENV",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests %v, want %v", requests, want)
	}
	if b := bodies[1]; b["visibility"] != "all" || b["value"] != "prod" {
		t.Errorf("create body %v", b)
	}
	if b := bodies[2]; len(b) != 1 || b["value"] != "staging" {
		t.Errorf("update body %v: an update must not change the visibility", b)
	}
	if v, err := c.Variables().Get(ctx, repo, "ENV"); err != nil || v.Value != "prod" || v.CreatedAt.IsZero() {
		t.Errorf("Get = %+v, %v", v, err)
	}
	if page, err := c.Variables().List(ctx, org, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("List = %+v, %v", page, err)
	}
	if err := c.Variables().Delete(ctx, repo, "ENV"); err != nil {
		t.Error(err)
	}
	if _, err := c.Variables().Get(ctx, gitoperationslib.UserCI(), "ENV"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user scope: err = %v, want ErrNotSupported", err)
	}
}
