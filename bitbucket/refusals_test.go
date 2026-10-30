package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"main", "main", true},
		{"main", "mainline", false},
		{"release/*", "release/1.0", true},
		{"release/*", "release/1.0/hotfix", true},
		{"release/*", "releases/1.0", false},
		{"v?.x", "v1.x", true},
		{"feat.x", "featyx", false}, // "." is literal
	}
	for _, tc := range cases {
		if got := globMatch(tc.pattern, tc.name); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// Bitbucket refuses to delete a restricted branch with HTTP 500 "Something
// went wrong"; the backend checks the restrictions before calling it a
// refusal.
func TestProtectedBranchDeleteBitbucket(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"type":"error","error":{"message":"Something went wrong"}}`)
		case r.URL.Path == "/2.0/repositories/acme/widget/branch-restrictions":
			io.WriteString(w, `{"values":[
				{"id":1,"kind":"delete","pattern":"release/*","branch_match_kind":"glob"},
				{"id":2,"kind":"force","pattern":"main","branch_match_kind":"glob"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()

	err := c.Branches().Delete(ctx, "acme", "widget", "release/1.0")
	if !errors.Is(err, gitoperationslib.ErrBranchProtected) || gitoperationslib.StatusCode(err) != 500 {
		t.Errorf("restricted branch: err = %v, want ErrBranchProtected with status 500", err)
	}
	// main only forbids force pushes: the 500 is not explained by a restriction.
	if err := c.Branches().Delete(ctx, "acme", "widget", "main"); errors.Is(err, gitoperationslib.ErrBranchProtected) {
		t.Errorf("unrestricted branch: err = %v, want a plain 500", err)
	}
}
