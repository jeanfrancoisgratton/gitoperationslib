package forgejo

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

func TestBranchProtectionRoundTrip(t *testing.T) {
	in := gitoperationslib.BranchProtection{
		Pattern: "release/*", RequiredApprovals: 2, DismissStaleApprovals: true,
		RequiredStatusChecks: []string{"ci/test"}, RequireUpToDate: true, RequireSignedCommits: true, ApplyToAdmins: true,
		PushAllowlist:          &gitoperationslib.Allowlist{Users: []string{"alice"}, Teams: []string{"devs"}},
		MergeAllowlist:         &gitoperationslib.Allowlist{Teams: []string{"leads"}},
		BlockOnRejectedReviews: true, ProtectedFilePatterns: "*.lock",
	}
	j, err := toBranchProtectionJSON(in, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !j.EnablePush || !j.EnablePushWhitelist || !j.EnableStatusCheck || !j.EnableMergeWhitelist || j.EnableApprovalsWhitelist ||
		fmt.Sprint(j.MergeWhitelistUsernames) != "[]" || fmt.Sprint(j.ApprovalsWhitelistTeams) != "[]" {
		t.Errorf("json = %+v", j)
	}
	out := j.toBranchProtection()
	if fmt.Sprintf("%+v %+v %+v", out.PushAllowlist, out.MergeAllowlist, out.ApprovalsAllowlist) != "&{Users:[alice] Teams:[devs]} &{Users:[] Teams:[leads]} <nil>" ||
		out.RequiredApprovals != 2 || !out.RequireUpToDate || !out.RequireSignedCommits || out.ProtectedFilePatterns != "*.lock" {
		t.Errorf("round trip = %+v", out)
	}

	// No direct pushes at all reads back as an empty allowlist.
	if got := (&branchProtectionJSON{EnablePush: false}).toBranchProtection(); got.PushAllowlist == nil || len(got.PushAllowlist.Users) != 0 {
		t.Errorf("push disabled = %+v", got.PushAllowlist)
	}
	// No checks: nil, not an empty list.
	if got := (&branchProtectionJSON{EnablePush: true, StatusCheckContexts: []string{"stale"}}).toBranchProtection(); got.RequiredStatusChecks != nil {
		t.Errorf("checks disabled = %v", got.RequiredStatusChecks)
	}
}

func TestBranchProtectionUnsupported(t *testing.T) {
	for name, rule := range map[string]gitoperationslib.BranchProtection{
		"deletion":     {Pattern: "main", AllowDeletion: true},
		"force push":   {Pattern: "main", AllowForcePush: true},
		"linear":       {Pattern: "main", RequireLinearHistory: true},
		"conversation": {Pattern: "main", RequireConversationResolution: true},
	} {
		if _, err := toBranchProtectionJSON(rule, "test"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	if _, err := c.BranchProtections().Update(context.Background(), "acme", "widget", "main", gitoperationslib.BranchProtection{Pattern: "dev"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("rename: err = %v", err)
	}
}

func TestProtectionRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/tag_protections") && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":4,"name_pattern":"v*","whitelist_usernames":["alice"]}]`)
		case strings.Contains(r.URL.Path, "/tag_protections"):
			io.WriteString(w, `{"id":4,"name_pattern":"v*","whitelist_usernames":["alice"]}`)
		case strings.HasSuffix(r.URL.Path, "/branch_protections") && r.Method == http.MethodGet:
			io.WriteString(w, `[{"rule_name":"main","enable_push":true}]`)
		default:
			io.WriteString(w, `{"rule_name":"release/*","enable_push":true,"required_approvals":1}`)
		}
	})
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	bp, tp := c.BranchProtections(), c.TagProtections()
	rules, err := bp.List(ctx, "acme", "widget")
	check(err)
	if len(rules) != 1 || rules[0].Pattern != "main" || rules[0].PushAllowlist != nil {
		t.Errorf("List = %+v", rules)
	}
	_, err = bp.Get(ctx, "acme", "widget", "release/*")
	check(err)
	_, err = bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{Pattern: "release/*", RequiredApprovals: 1})
	check(err)
	_, err = bp.Update(ctx, "acme", "widget", "release/*", gitoperationslib.BranchProtection{RequiredApprovals: 1})
	check(err)
	check(bp.Delete(ctx, "acme", "widget", "release/*"))
	tag, err := tp.Get(ctx, "acme", "widget", "v*")
	check(err)
	if tag.ID != 4 || fmt.Sprint(tag.Allowlist.Users) != "[alice]" {
		t.Errorf("tag Get = %+v", tag)
	}
	_, err = tp.Create(ctx, "acme", "widget", gitoperationslib.TagProtection{Pattern: "v*", Allowlist: gitoperationslib.Allowlist{Users: []string{"alice"}}})
	check(err)
	_, err = tp.Update(ctx, "acme", "widget", "v*", gitoperationslib.TagProtection{Allowlist: gitoperationslib.Allowlist{Teams: []string{"devs"}}})
	check(err)
	check(tp.Delete(ctx, "acme", "widget", "v*"))
	if _, err := tp.Get(ctx, "acme", "widget", "other*"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("missing tag rule: err = %v", err)
	}

	wantRequests := []string{
		"GET /api/v1/repos/acme/widget/branch_protections",
		"GET /api/v1/repos/acme/widget/branch_protections/release%2F%2A",
		"POST /api/v1/repos/acme/widget/branch_protections",
		"PATCH /api/v1/repos/acme/widget/branch_protections/release%2F%2A",
		"DELETE /api/v1/repos/acme/widget/branch_protections/release%2F%2A",
		"GET /api/v1/repos/acme/widget/tag_protections",
		"POST /api/v1/repos/acme/widget/tag_protections",
		"GET /api/v1/repos/acme/widget/tag_protections",
		"PATCH /api/v1/repos/acme/widget/tag_protections/4",
		"GET /api/v1/repos/acme/widget/tag_protections",
		"DELETE /api/v1/repos/acme/widget/tag_protections/4",
		"GET /api/v1/repos/acme/widget/tag_protections",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	if bodies[2]["rule_name"] != "release/*" || bodies[2]["required_approvals"] != 1.0 || bodies[2]["enable_push"] != true {
		t.Errorf("create body = %v", bodies[2])
	}
	if _, ok := bodies[3]["rule_name"]; ok {
		t.Errorf("update body must not carry rule_name: %v", bodies[3])
	}
	if fmt.Sprint(bodies[8]) != "map[name_pattern:v* whitelist_teams:[devs] whitelist_usernames:[]]" {
		t.Errorf("tag update body = %v", bodies[8])
	}
}
