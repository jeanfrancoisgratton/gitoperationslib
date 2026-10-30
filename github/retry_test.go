package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fastRetry keeps backoff waits in the milliseconds.
var fastRetry = gitoperationslib.RetryPolicy{MaxAttempts: 3, MaxWait: 5 * time.Millisecond}

func retryServer(t *testing.T, handler http.HandlerFunc, policy gitoperationslib.RetryPolicy) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New("pat", WithEnterpriseURL(srv.URL, ""), WithHTTPClient(srv.Client()), WithRetry(policy))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConflictInProgressRetried(t *testing.T) {
	calls := 0
	c := retryServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"message":"Conflicting repository operation is still in progress","documentation_url":"https://docs.github.com/rest"}`)
			return
		}
		io.WriteString(w, repoResponse)
	}, fastRetry)
	desc := "new"
	if _, err := c.Repos().Edit(context.Background(), "acme", "widget", gitoperationslib.EditRepoOptions{Description: &desc}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if calls != 2 {
		t.Errorf("%d requests, want 2", calls)
	}
}

func TestRateLimitRetryAt(t *testing.T) {
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	c := retryServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"API rate limit exceeded for user ID 1."}`)
	}, gitoperationslib.DefaultRetryPolicy)
	_, err := c.Repos().Get(context.Background(), "acme", "widget")
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if !apiErr.RetryAt.Equal(reset) {
		t.Errorf("RetryAt = %v, want %v", apiErr.RetryAt, reset)
	}
}

func TestProtectedBranchDelete(t *testing.T) {
	c := retryServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		switch r.URL.Path {
		case "/api/v3/repos/acme/widget/git/refs/heads/main":
			io.WriteString(w, `{"message":"Repository rule violations found\n\nCannot delete this branch\n\n","status":"422"}`)
		default:
			io.WriteString(w, `{"message":"Reference does not exist"}`)
		}
	}, gitoperationslib.NoRetry)
	ctx := context.Background()
	if err := c.Branches().Delete(ctx, "acme", "widget", "main"); !errors.Is(err, gitoperationslib.ErrBranchProtected) {
		t.Errorf("protected: err = %v, want ErrBranchProtected", err)
	}
	if err := c.Branches().Delete(ctx, "acme", "widget", "gone"); errors.Is(err, gitoperationslib.ErrBranchProtected) || gitoperationslib.StatusCode(err) != 422 {
		t.Errorf("missing ref: err = %v, want a plain 422", err)
	}
}
