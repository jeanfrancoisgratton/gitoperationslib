package forgejo

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// countedPages answers like most Forgejo lists: pages and X-Total-Count, no
// Link header. The server caps the page size at 2, whatever limit is asked for.
func countedPages(w http.ResponseWriter, r *http.Request, item string, total int) {
	limit, page := 2, 1
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v < limit {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		page = v
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	var items []string
	for i := (page - 1) * limit; i < page*limit && i < total; i++ {
		items = append(items, strings.ReplaceAll(item, "%d", strconv.Itoa(i+1)))
	}
	io.WriteString(w, "["+strings.Join(items, ",")+"]")
}

// collect returns how many items All yields.
func collect[T any](t *testing.T, opts gitoperationslib.ListOptions, fetch func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[T], error)) int {
	t.Helper()
	n := 0
	for _, err := range gitoperationslib.All(context.Background(), opts, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	return n
}

func TestAllWithoutLinkHeader(t *testing.T) {
	const total = 5
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		item := `{"id":%d,"name":"n%d","login":"n%d","key_id":"n%d","data":"v"}`
		if strings.HasSuffix(r.URL.Path, "/tags") {
			item = `{"name":"n%d","commit":{"sha":"s"}}`
		}
		countedPages(w, r, item, total)
	})
	repo, org := gitoperationslib.RepoCI("acme", "widget"), gitoperationslib.OrgCI("acme")
	const owner, name = "acme", "widget"

	tests := map[string]func(gitoperationslib.ListOptions) int{
		"labels": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
				return c.Labels().List(ctx, owner, name, o)
			})
		},
		"milestones": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
				return c.Milestones().List(ctx, owner, name, gitoperationslib.MilestoneStateOpen, o)
			})
		},
		"tags": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Tag], error) {
				return c.Tags().List(ctx, owner, name, o)
			})
		},
		"repository webhooks": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Webhook], error) {
				return c.Webhooks().List(ctx, gitoperationslib.RepoHooks(owner, name), o)
			})
		},
		"organisation members": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
				return c.Orgs().Members(ctx, owner, o)
			})
		},
		"teams": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
				return c.Teams().List(ctx, owner, o)
			})
		},
		"team members": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
				return teams{c}.membersByID(ctx, 1, o)
			})
		},
		"team repositories": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
				return teams{c}.reposByID(ctx, 1, o)
			})
		},
		"secrets": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
				return c.CI().Secrets().List(ctx, org, o)
			})
		},
		"variables": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
				return c.CI().Variables().List(ctx, repo, o)
			})
		},
		"SSH keys": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, c.Account().SSHKeys().List)
		},
		"reviews": func(o gitoperationslib.ListOptions) int {
			return collect(t, o, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Review], error) {
				return c.PullRequests().ListReviews(ctx, owner, name, 1, o)
			})
		},
	}
	for what, count := range tests {
		// The zero value and 100 leave the page size to the server's cap.
		for _, opts := range []gitoperationslib.ListOptions{{}, {PerPage: 1}, {PerPage: 2}, {PerPage: 100}} {
			if got := count(opts); got != total {
				t.Errorf("%s with %+v: All returned %d items, want %d", what, opts, got, total)
			}
		}
	}
}

func TestLabelIDsBeyondFirstPage(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		countedPages(w, r, `{"id":%d,"name":"label-%d"}`, 5)
	})
	ids, err := c.labelIDs(context.Background(), "acme", "widget", []string{"label-5"})
	if err != nil || !slices.Equal(ids, []int64{5}) {
		t.Errorf("labelIDs = %v, %v", ids, err)
	}
}

func TestMergeRetriedWhileChecking(t *testing.T) {
	attempts := 0
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusMethodNotAllowed)
			io.WriteString(w, `{"message":"Please try again later"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.PullRequests().Merge(context.Background(), "acme", "widget", 1, gitoperationslib.MergeOptions{}); err != nil {
		t.Errorf("Merge: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestMergeRefusalNotRetried(t *testing.T) {
	attempts := 0
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusMethodNotAllowed)
		io.WriteString(w, `{"message":"The PR is a draft"}`)
	})
	err := c.PullRequests().Merge(context.Background(), "acme", "widget", 1, gitoperationslib.MergeOptions{})
	if gitoperationslib.StatusCode(err) != http.StatusMethodNotAllowed || attempts != 1 {
		t.Errorf("err = %v after %d attempts, want one 405", err, attempts)
	}
}
