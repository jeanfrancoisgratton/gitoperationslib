package bitbucket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestDeploymentVariablesBitbucket(t *testing.T) {
	const envs = "/2.0/repositories/acme/widget/environments"
	const vars = "/2.0/repositories/acme/widget/deployments_config/environments/{e1}/variables"
	var requests []string
	var bodies []pipelineVarJSON
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body pipelineVarJSON
		json.NewDecoder(r.Body).Decode(&body)
		switch r.Method + " " + r.URL.Path {
		case "GET " + envs:
			io.WriteString(w, `{"values": [{"uuid": "{e0}", "name": "Test", "slug": "test"}, {"uuid": "{e1}", "name": "Prod EU", "slug": "prod-eu"}]}`)
		case "GET " + vars:
			io.WriteString(w, `{"values": [{"uuid": "{v1}", "key": "ENV", "value": "prod", "secured": false}, {"uuid": "{s1}", "key": "TOKEN", "secured": true}]}`)
		case "POST " + vars:
			if body.Key == "ENV" {
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"error": {"message": "Conflict"}}`)
				return
			}
			bodies = append(bodies, body)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{}`)
		case "PUT " + vars + "/{v1}":
			bodies = append(bodies, body)
			io.WriteString(w, `{}`)
		case "DELETE " + vars + "/{s1}":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	// By name, by slug, ignoring case.
	for _, name := range []string{"Prod EU", "prod-eu", "PROD eu"} {
		v, err := c.CI().Variables().Get(ctx, gitoperationslib.EnvCI("acme", "widget", name), "ENV")
		if err != nil || v.Value != "prod" {
			t.Errorf("Get in %q = %+v, %v", name, v, err)
		}
	}
	// By UUID, with no lookup.
	requests = nil
	scope := gitoperationslib.EnvCI("acme", "widget", "{e1}")
	secrets, err := c.CI().Secrets().List(ctx, scope, gitoperationslib.ListOptions{})
	if err != nil || len(secrets.Items) != 1 || secrets.Items[0].Name != "TOKEN" {
		t.Errorf("Secrets.List = %+v, %v", secrets, err)
	}
	if !slices.Equal(requests, []string{"GET " + vars}) {
		t.Errorf("requests = %q", requests)
	}

	// Writing looks the environment up once, even when the entry exists.
	requests = nil
	byName := gitoperationslib.EnvCI("acme", "widget", "prod-eu")
	if err := c.CI().Variables().Set(ctx, byName, "ENV", "new"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET " + envs, "POST " + vars, "GET " + vars, "PUT " + vars + "/{v1}"}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %q, want %q", requests, want)
	}
	if err := c.CI().Secrets().Set(ctx, byName, "NEW", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if last := bodies[len(bodies)-1]; last.Key != "NEW" || !last.Secured || last.Value != "s3cret" {
		t.Errorf("secret sent = %+v", last)
	}
	if err := c.CI().Secrets().Delete(ctx, byName, "TOKEN"); err != nil {
		t.Error(err)
	}
	// One list for both kinds, as elsewhere.
	if err := c.CI().Secrets().Set(ctx, byName, "ENV", "x"); err == nil || !strings.Contains(err.Error(), "is a variable") {
		t.Errorf("secret named like a variable: err = %v", err)
	}
}

func TestDeploymentVariablesUnknownEnvironment(t *testing.T) {
	lookups := 0
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/repositories/acme/widget/environments" {
			t.Errorf("unexpected %s", r.URL)
		}
		lookups++
		io.WriteString(w, `{"values": []}`)
	})
	err := c.CI().Variables().Set(context.Background(), gitoperationslib.EnvCI("acme", "widget", "nope"), "ENV", "x")
	if !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v, want 404", err)
	}
	if lookups != findAttempts { // the list lags behind a new environment
		t.Errorf("%d lookups, want %d", lookups, findAttempts)
	}
}

func TestDeploymentVariablesLaggingEnvironment(t *testing.T) {
	lookups := 0
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/environments") {
			if lookups++; lookups == 1 {
				io.WriteString(w, `{"values": []}`)
				return
			}
			io.WriteString(w, `{"values": [{"uuid": "{e1}", "name": "New", "slug": "new"}]}`)
			return
		}
		io.WriteString(w, `{"values": []}`)
	})
	if _, err := c.CI().Variables().List(context.Background(), gitoperationslib.EnvCI("acme", "widget", "New"), gitoperationslib.ListOptions{}); err != nil {
		t.Error(err)
	}
}

func TestEnvironmentScopeNeedsARepositoryBitbucket(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("request sent: %s", r.URL) })
	for _, scope := range []gitoperationslib.CIScope{{Owner: "acme", Environment: "prod"}, {Environment: "prod"}} {
		if err := c.CI().Variables().Set(context.Background(), scope, "X", "x"); err == nil {
			t.Errorf("Set(%+v) succeeded", scope)
		}
	}
}
