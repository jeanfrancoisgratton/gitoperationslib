package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCIBitbucket(t *testing.T) {
	var requests []string
	var bodies []pipelineVarJSON
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body pipelineVarJSON
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		const list = `{"values": [
			{"uuid": "{v1}", "key": "ENV", "value": "prod", "secured": false},
			{"uuid": "{s1}", "key": "TOKEN", "secured": true}]}`
		switch r.Method + " " + r.URL.Path {
		case "GET /2.0/repositories/acme/widget/pipelines_config/variables", "GET /2.0/workspaces/acme/pipelines-config/variables":
			io.WriteString(w, list)
		case "POST /2.0/repositories/acme/widget/pipelines_config/variables":
			if body.Key == "ENV" || body.Key == "TOKEN" {
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"error": {"message": "Conflict"}}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{}`)
		case "PUT /2.0/repositories/acme/widget/pipelines_config/variables/{s1}",
			"DELETE /2.0/workspaces/acme/pipelines-config/variables/{v1}":
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	repo, ws := gitoperationslib.RepoCI("acme", "widget"), gitoperationslib.OrgCI("acme")

	// New name: one POST, secured.
	if err := c.Secrets().Set(ctx, repo, "NEW", "x"); err != nil || !bodies[0].Secured || requests[0] != "POST /2.0/repositories/acme/widget/pipelines_config/variables" {
		t.Errorf("Set new secret: %v, %v %+v", err, requests, bodies)
	}
	// Existing secret: POST 409, list, PUT by UUID.
	requests, bodies = nil, nil
	if err := c.Secrets().Set(ctx, repo, "TOKEN", "y"); err != nil || len(requests) != 3 || !bodies[2].Secured || bodies[2].Value != "y" {
		t.Errorf("overwrite secret: %v, %v %+v", err, requests, bodies)
	}
	// A variable cannot take a secret's name.
	if err := c.Variables().Set(ctx, repo, "TOKEN", "z"); err == nil || !strings.Contains(err.Error(), "is a secret") {
		t.Errorf("variable named like a secret: err = %v", err)
	}

	secrets, err := c.Secrets().List(ctx, repo, gitoperationslib.ListOptions{})
	if err != nil || len(secrets.Items) != 1 || secrets.Items[0].Name != "TOKEN" {
		t.Errorf("Secrets.List = %+v, %v", secrets, err)
	}
	vars, err := c.Variables().List(ctx, ws, gitoperationslib.ListOptions{})
	if err != nil || len(vars.Items) != 1 || vars.Items[0].Value != "prod" {
		t.Errorf("Variables.List = %+v, %v", vars, err)
	}
	if v, err := c.Variables().Get(ctx, ws, "ENV"); err != nil || v.Value != "prod" {
		t.Errorf("Get = %+v, %v", v, err)
	}
	if err := c.Variables().Delete(ctx, ws, "ENV"); err != nil {
		t.Error(err)
	}
	if err := c.Secrets().Set(ctx, gitoperationslib.UserCI(), "X", "x"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user scope: err = %v, want ErrNotSupported", err)
	}
}

// A name that never shows up is reported missing after the lag retries.
func TestCIBitbucketNotFound(t *testing.T) {
	lists := 0
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		lists++
		io.WriteString(w, `{"values": []}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Variables().Get(ctx, gitoperationslib.RepoCI("acme", "widget"), "NOPE"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v, want 404", err)
	}
	if lists != findAttempts {
		t.Errorf("%d listings, want %d", lists, findAttempts)
	}
}
