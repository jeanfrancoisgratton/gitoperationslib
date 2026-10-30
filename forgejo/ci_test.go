package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCIForgejo(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch r.Method + " " + r.URL.Path {
		case "PUT /api/v1/repos/acme/widget/actions/secrets/TOKEN":
			w.WriteHeader(http.StatusCreated)
		case "GET /api/v1/orgs/acme/actions/secrets":
			io.WriteString(w, `[{"name": "TOKEN", "created_at": "2026-10-30T12:24:42Z"}]`)
		case "DELETE /api/v1/user/actions/secrets/TOKEN", "POST /api/v1/user/actions/variables/ENV", "DELETE /api/v1/orgs/acme/actions/variables/ENV":
			w.WriteHeader(http.StatusNoContent)
		case "PUT /api/v1/user/actions/variables/ENV":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message": "variable not found"}`)
		case "GET /api/v1/repos/acme/widget/actions/variables/ENV":
			io.WriteString(w, `{"owner_id": 0, "repo_id": 75, "name": "ENV", "data": "prod"}`)
		case "GET /api/v1/repos/acme/widget/actions/variables":
			io.WriteString(w, `[{"name": "ENV", "data": "prod"}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	repo, org, user := gitoperationslib.RepoCI("acme", "widget"), gitoperationslib.OrgCI("acme"), gitoperationslib.UserCI()

	if err := c.Secrets().Set(ctx, repo, "TOKEN", "s3cr3t"); err != nil {
		t.Error(err)
	}
	if page, err := c.Secrets().List(ctx, org, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 || page.Items[0].CreatedAt.IsZero() {
		t.Errorf("List = %+v, %v", page, err)
	}
	if err := c.Secrets().Delete(ctx, user, "TOKEN"); err != nil {
		t.Error(err)
	}
	if err := c.Variables().Set(ctx, user, "ENV", "prod"); err != nil {
		t.Error(err)
	}
	if v, err := c.Variables().Get(ctx, repo, "ENV"); err != nil || v.Value != "prod" {
		t.Errorf("Get = %+v, %v", v, err)
	}
	if page, err := c.Variables().List(ctx, repo, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("Variables.List = %+v, %v", page, err)
	}
	if err := c.Variables().Delete(ctx, org, "ENV"); err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(bodies[0], map[string]any{"data": "s3cr3t"}) || !reflect.DeepEqual(bodies[4], map[string]any{"value": "prod"}) {
		t.Errorf("bodies %v", bodies)
	}
	if requests[3] != "PUT /api/v1/user/actions/variables/ENV" || requests[4] != "POST /api/v1/user/actions/variables/ENV" {
		t.Errorf("Set of a new variable: %v, want PUT then POST", requests[3:5])
	}

	n := len(requests)
	if _, err := c.Secrets().List(ctx, user, gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user secrets List: err = %v, want ErrNotSupported", err)
	}
	if err := c.Secrets().Set(ctx, repo, "", "x"); err == nil {
		t.Error("empty name accepted")
	}
	if err := c.Variables().Set(ctx, gitoperationslib.CIScope{Repo: "widget"}, "ENV", "x"); err == nil {
		t.Error("repository scope without owner accepted")
	}
	if len(requests) != n {
		t.Errorf("refused calls sent %v", requests[n:])
	}
}
