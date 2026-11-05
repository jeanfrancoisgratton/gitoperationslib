package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// restrictionServer starts with rule "main" (restrictions 1 and 2) and rule
// "dev" (restriction 3), and records writes in order.
func restrictionServer(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var writes []string
	stored := []restrictionJSON{
		{ID: 1, Kind: kindDelete, Pattern: "main", BranchMatchKind: "glob"},
		{ID: 2, Kind: kindForce, Pattern: "main", BranchMatchKind: "glob"},
		{ID: 3, Kind: kindDelete, Pattern: "dev", BranchMatchKind: "glob"},
	}
	next := int64(100)
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch r.Method {
		case http.MethodDelete:
			writes = append(writes, "DELETE "+id)
			stored = slices.DeleteFunc(stored, func(s restrictionJSON) bool { return fmt.Sprint(s.ID) == id })
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			var rj restrictionJSON
			json.NewDecoder(r.Body).Decode(&rj)
			writes = append(writes, "POST "+rj.Kind+" "+rj.Pattern)
			next++
			rj.ID = next
			stored = append(stored, rj)
			json.NewEncoder(w).Encode(rj)
		default:
			json.NewEncoder(w).Encode(map[string]any{"values": stored})
		}
	})
	return c, &writes
}

func TestUpdateProtectionBitbucket(t *testing.T) {
	c, writes := restrictionServer(t)
	ctx := context.Background()

	// The old restrictions go first; an empty Pattern keeps the rule's pattern;
	// the other rule is left alone.
	rule, err := c.BranchProtections().Update(ctx, "acme", "widget", "main", gitoperationslib.BranchProtection{AllowForcePush: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DELETE 1", "DELETE 2", "POST delete main"}
	if !slices.Equal(*writes, want) {
		t.Errorf("writes = %v, want %v", *writes, want)
	}
	if rule.Pattern != "main" || !rule.AllowForcePush || rule.AllowDeletion {
		t.Errorf("rule = %+v", rule)
	}

	// A new Pattern moves the rule.
	*writes = nil
	moved, err := c.BranchProtections().Update(ctx, "acme", "widget", "dev", gitoperationslib.BranchProtection{Pattern: "develop", AllowForcePush: true})
	if err != nil || moved.Pattern != "develop" {
		t.Errorf("moved = %+v, %v", moved, err)
	}
	if !slices.Equal(*writes, []string{"DELETE 3", "POST delete develop"}) {
		t.Errorf("writes = %v", *writes)
	}
}

// An invalid rule must not delete the existing one.
func TestUpdateProtectionRefusedBitbucket(t *testing.T) {
	c, writes := restrictionServer(t)
	ctx := context.Background()
	bp := c.BranchProtections()

	if _, err := bp.Update(ctx, "acme", "widget", "main", gitoperationslib.BranchProtection{RequireSignedCommits: true}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("unsupported option: err = %v", err)
	}
	if _, err := bp.Update(ctx, "acme", "widget", "main", gitoperationslib.BranchProtection{AllowDeletion: true, AllowForcePush: true}); err == nil || !strings.Contains(err.Error(), "restricts nothing") {
		t.Errorf("empty rule: err = %v", err)
	}
	if _, err := bp.Update(ctx, "acme", "widget", "none", gitoperationslib.BranchProtection{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown pattern: err = %v, want 404", err)
	}
	if len(*writes) != 0 {
		t.Errorf("refused calls sent %v", *writes)
	}
}

func TestDeleteProtectionBitbucket(t *testing.T) {
	c, writes := restrictionServer(t)
	ctx := context.Background()
	if err := c.BranchProtections().Delete(ctx, "acme", "widget", "main"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*writes, []string{"DELETE 1", "DELETE 2"}) {
		t.Errorf("writes = %v, want only rule main's restrictions", *writes)
	}
	if err := c.BranchProtections().Delete(ctx, "acme", "widget", "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown pattern: err = %v, want 404", err)
	}
}

func TestProtectionMoreOptionsBitbucket(t *testing.T) {
	var posted []restrictionJSON
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var rj restrictionJSON
			json.NewDecoder(r.Body).Decode(&rj)
			posted = append(posted, rj)
			json.NewEncoder(w).Encode(rj)
			return
		}
		io.WriteString(w, `{"values":[]}`)
	})
	ctx := context.Background()
	bp := c.BranchProtections()

	if _, err := bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{}); err == nil {
		t.Error("rule without a pattern accepted")
	}
	// An allowlist naming nobody still restricts: no one may merge.
	_, err := bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{
		Pattern: "main", AllowDeletion: true, AllowForcePush: true, MergeAllowlist: &gitoperationslib.Allowlist{},
	})
	if !gitoperationslib.IsNotFound(err) { // the fake lists nothing back
		t.Errorf("err = %v, want the 404 of reading the rule back", err)
	}
	if len(posted) != 1 || posted[0].Kind != kindMerge {
		t.Fatalf("posted = %+v", posted)
	}

	posted = nil
	if _, err := bp.Create(ctx, "acme", "widget", gitoperationslib.BranchProtection{
		Pattern: "main", PushAllowlist: &gitoperationslib.Allowlist{Users: []string{"nobody"}},
	}); err == nil {
		t.Error("unknown user accepted")
	}
	if len(posted) != 0 {
		t.Errorf("unknown user: posted %+v before resolving everyone", posted)
	}

	for name, rule := range map[string]gitoperationslib.BranchProtection{
		"status checks":       {RequiredStatusChecks: []string{"ci"}},
		"up to date":          {RequireUpToDate: true},
		"signed commits":      {RequireSignedCommits: true},
		"linear history":      {RequireLinearHistory: true},
		"conversations":       {RequireConversationResolution: true},
		"approvals allowlist": {ApprovalsAllowlist: &gitoperationslib.Allowlist{}},
		"rejected reviews":    {BlockOnRejectedReviews: true},
		"official requests":   {BlockOnOfficialReviewRequests: true},
		"protected files":     {ProtectedFilePatterns: "*.go"},
		"unprotected files":   {UnprotectedFilePatterns: "*.md"},
	} {
		rule.Pattern = "main"
		if _, err := bp.Create(ctx, "acme", "widget", rule); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if len(posted) != 0 {
		t.Errorf("refused calls posted %+v", posted)
	}
}
