package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// TestRefusals checks that refusals are recognised from the responses
// Forgejo 15.0.11 actually sends (captured on the disposable instance).
func TestRefusals(t *testing.T) {
	type reply struct {
		status int
		msg    string
	}
	replies := map[string]reply{
		"DELETE /api/v1/orgs/acme":                       {500, "user still has ownership of repositories [uid: 2]"},
		"DELETE /api/v1/orgs/acme/members/alice":         {500, "user is the last member of owner team [uid: 1]"},
		"DELETE /api/v1/teams/7/members/alice":           {500, "user is the last member of owner team [uid: 1]"},
		"DELETE /api/v1/admin/users/owner1":              {422, "user still has ownership of repositories [uid: 3]"},
		"DELETE /api/v1/admin/users/owner2":              {422, "user still has membership of organizations [uid: 4]"},
		"DELETE /api/v1/repos/acme/r1/branches/keep":     {403, "branch protected"},
		"DELETE /api/v1/repos/acme/r1/branches/main":     {403, "can not delete default branch"},
		"DELETE /api/v1/orgs/other":                      {500, "something else broke"},
		"DELETE /api/v1/admin/users/someone/unexpected/": {404, "not found"},
	}
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		rep, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(rep.status)
		fmt.Fprintf(w, `{"message":%q}`, rep.msg)
	})
	ctx := context.Background()

	cases := []struct {
		name   string
		err    error
		want   error // nil: no sentinel
		status int
	}{
		{"org owning repos", c.Orgs().Delete(ctx, "acme"), gitoperationslib.ErrOwnsRepos, 500},
		{"last owner, org", c.Orgs().RemoveMember(ctx, "acme", "alice"), gitoperationslib.ErrLastOwner, 500},
		{"last owner, team", c.Teams().RemoveMember(ctx, 7, "alice"), gitoperationslib.ErrLastOwner, 500},
		{"user owning repos", c.Users().Delete(ctx, "owner1", gitoperationslib.DeleteUserOptions{}), gitoperationslib.ErrOwnsRepos, 422},
		{"user in orgs", c.Users().Delete(ctx, "owner2", gitoperationslib.DeleteUserOptions{}), gitoperationslib.ErrOrgMember, 422},
		{"protected branch", c.Branches().Delete(ctx, "acme", "r1", "keep"), gitoperationslib.ErrBranchProtected, 403},
		{"default branch", c.Branches().Delete(ctx, "acme", "r1", "main"), nil, 403},
		{"other 500", c.Orgs().Delete(ctx, "other"), nil, 500},
	}
	sentinels := []error{gitoperationslib.ErrOwnsRepos, gitoperationslib.ErrOrgMember, gitoperationslib.ErrLastOwner, gitoperationslib.ErrBranchProtected}
	for _, tc := range cases {
		if got := gitoperationslib.StatusCode(tc.err); got != tc.status {
			t.Errorf("%s: status %d, want %d (the server's status is kept)", tc.name, got, tc.status)
		}
		for _, s := range sentinels {
			if errors.Is(tc.err, s) != (s == tc.want) {
				t.Errorf("%s: errors.Is(err, %q) = %v (err: %v)", tc.name, s, errors.Is(tc.err, s), tc.err)
			}
		}
	}
}
