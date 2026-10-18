package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// rulesetServer stores rulesets created through the API in memory and
// answers the team lookups used for bypass actors (team "devs" = ID 42).
func rulesetServer(t *testing.T) (*Client, map[int64]map[string]any) {
	t.Helper()
	store := map[int64]map[string]any{}
	var next int64 = 100
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/v3/orgs/acme/teams/devs":
			io.WriteString(w, `{"id":42,"slug":"devs"}`)
		case p == "/api/v3/repos/acme/widget/teams":
			io.WriteString(w, `[{"id":42,"slug":"devs"}]`)
		case p == "/api/v3/repos/acme/widget/rulesets" && r.Method == http.MethodPost:
			var rs map[string]any
			json.NewDecoder(r.Body).Decode(&rs)
			next++
			rs["id"] = next
			store[next] = rs
			json.NewEncoder(w).Encode(rs)
		case p == "/api/v3/repos/acme/widget/rulesets":
			var list []map[string]any
			for id, rs := range store {
				list = append(list, map[string]any{"id": id, "name": rs["name"], "target": rs["target"]})
			}
			json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(p, "/api/v3/repos/acme/widget/rulesets/"):
			var id int64
			fmt.Sscan(strings.TrimPrefix(p, "/api/v3/repos/acme/widget/rulesets/"), &id)
			switch r.Method {
			case http.MethodDelete:
				delete(store, id)
				w.WriteHeader(http.StatusNoContent)
			case http.MethodPut:
				var rs map[string]any
				json.NewDecoder(r.Body).Decode(&rs)
				rs["id"] = id
				store[id] = rs
				json.NewEncoder(w).Encode(rs)
			default:
				json.NewEncoder(w).Encode(store[id])
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, p)
		}
	})
	return c, store
}

func TestBranchRulesetMapping(t *testing.T) {
	c, store := rulesetServer(t)
	ctx := context.Background()
	bp := c.BranchProtections()

	created, err := bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{
		Pattern: "main", RequiredApprovals: 2, DismissStaleApprovals: true,
		RequiredStatusChecks: []string{"ci/test"}, RequireUpToDate: true, RequireSignedCommits: true,
		RequireLinearHistory: true, PushAllowlist: &gitoperationslib.Allowlist{Teams: []string{"devs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := store[created.ID]
	rules, _ := json.Marshal(raw["rules"])
	for _, want := range []string{`"type":"deletion"`, `"type":"non_fast_forward"`, `"type":"pull_request"`, `"required_approving_review_count":2`,
		`"type":"required_status_checks"`, `"strict_required_status_checks_policy":true`, `"type":"required_signatures"`, `"type":"required_linear_history"`} {
		if !strings.Contains(string(rules), want) {
			t.Errorf("rules lack %s: %s", want, rules)
		}
	}
	actors, _ := json.Marshal(raw["bypass_actors"])
	if !strings.Contains(string(actors), `"actor_id":5,"actor_type":"RepositoryRole"`) || !strings.Contains(string(actors), `"actor_id":42,"actor_type":"Team"`) {
		t.Errorf("bypass actors = %s", actors)
	}
	if fmt.Sprint(raw["conditions"]) != "map[ref_name:map[exclude:[] include:[refs/heads/main]]]" || raw["target"] != "branch" || raw["enforcement"] != "active" {
		t.Errorf("ruleset = %v", raw)
	}

	got, err := bp.Get(ctx, "acme", "widget", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pattern != "main" || got.RequiredApprovals != 2 || !got.DismissStaleApprovals || fmt.Sprint(got.RequiredStatusChecks) != "[ci/test]" ||
		!got.RequireUpToDate || got.ApplyToAdmins || got.AllowDeletion || got.AllowForcePush || !got.RequireLinearHistory ||
		got.PushAllowlist == nil || fmt.Sprint(got.PushAllowlist.Teams) != "[devs]" {
		t.Errorf("Get = %+v", got)
	}

	// ApplyToAdmins drops the admin bypass; allowing deletion drops the rule.
	got.ApplyToAdmins, got.AllowDeletion, got.PushAllowlist = true, true, nil
	got.RequiredApprovals, got.DismissStaleApprovals = 0, false
	updated, err := bp.Update(ctx, "acme", "widget", "main", *got)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ApplyToAdmins || !updated.AllowDeletion || updated.PushAllowlist != nil {
		t.Errorf("updated = %+v", updated)
	}
	if list, err := bp.List(ctx, "acme", "widget"); err != nil || len(list) != 1 {
		t.Errorf("List = %+v, %v", list, err)
	}
	if err := bp.Delete(ctx, "acme", "widget", "main"); err != nil || len(store) != 0 {
		t.Errorf("Delete: %v, store = %v", err, store)
	}
	if _, err := bp.Get(ctx, "acme", "widget", "main"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("missing rule: err = %v", err)
	}
}

func TestBranchRulesetUnsupported(t *testing.T) {
	c, _ := rulesetServer(t)
	ctx := context.Background()
	for name, rule := range map[string]gitoperationslib.BranchProtection{
		"user allowlist":   {Pattern: "main", PushAllowlist: &gitoperationslib.Allowlist{Users: []string{"alice"}}},
		"merge allowlist":  {Pattern: "main", MergeAllowlist: &gitoperationslib.Allowlist{}},
		"rejected reviews": {Pattern: "main", BlockOnRejectedReviews: true},
		"file patterns":    {Pattern: "main", ProtectedFilePatterns: "*.lock"},
		"up to date alone": {Pattern: "main", RequireUpToDate: true},
	} {
		if _, err := c.BranchProtections().Create(ctx, "acme", "widget", rule); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTagRulesetMapping(t *testing.T) {
	c, store := rulesetServer(t)
	ctx := context.Background()
	tp := c.TagProtections()

	created, err := tp.Create(ctx, "acme", "widget", gitoperationslib.TagProtection{Pattern: "v*", Allowlist: gitoperationslib.Allowlist{Teams: []string{"devs"}}})
	if err != nil {
		t.Fatal(err)
	}
	raw := store[created.ID]
	rules, _ := json.Marshal(raw["rules"])
	for _, want := range []string{`"type":"creation"`, `"type":"update"`, `"type":"deletion"`} {
		if !strings.Contains(string(rules), want) {
			t.Errorf("rules lack %s: %s", want, rules)
		}
	}
	if raw["target"] != "tag" || fmt.Sprint(raw["conditions"]) != "map[ref_name:map[exclude:[] include:[refs/tags/v*]]]" {
		t.Errorf("ruleset = %v", raw)
	}
	// Tag rules exempt no admin role: only the allowlisted teams bypass.
	if actors, _ := json.Marshal(raw["bypass_actors"]); strings.Contains(string(actors), "RepositoryRole") {
		t.Errorf("tag rule exempts the admin role: %s", actors)
	}
	if created.Pattern != "v*" || fmt.Sprint(created.Allowlist.Teams) != "[devs]" {
		t.Errorf("created = %+v", created)
	}

	moved, err := tp.Update(ctx, "acme", "widget", "v*", gitoperationslib.TagProtection{Pattern: "release-*"})
	if err != nil || moved.Pattern != "release-*" {
		t.Fatalf("Update = %+v, %v", moved, err)
	}
	if _, err := tp.Get(ctx, "acme", "widget", "v*"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("old pattern: err = %v", err)
	}
	if _, err := tp.Create(ctx, "acme", "widget", gitoperationslib.TagProtection{Pattern: "x*", Allowlist: gitoperationslib.Allowlist{Users: []string{"alice"}}}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user allowlist: err = %v", err)
	}
}
