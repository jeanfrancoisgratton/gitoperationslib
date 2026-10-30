package retry

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fakeNow is the clock the tests run at.
var fakeNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// newClient returns a client retrying with policy, recording each wait
// instead of sleeping.
func newClient(policy gitoperationslib.RetryPolicy, conflicts ...string) (*http.Client, *[]time.Duration) {
	waits := &[]time.Duration{}
	c := Wrap(nil, policy, conflicts...)
	tr := c.Transport.(*Transport)
	tr.sleep = func(_ context.Context, d time.Duration) error { *waits = append(*waits, d); return nil }
	tr.now = func() time.Time { return fakeNow }
	return c, waits
}

// script serves the given statuses in turn (the last one repeats), with
// headers, and counts the requests; each request's body is recorded.
func script(t *testing.T, statuses []int, header http.Header, body string) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var n atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(data))
		i := int(n.Add(1)) - 1
		status := statuses[min(i, len(statuses)-1)]
		if status != http.StatusOK {
			for k, v := range header {
				w.Header()[k] = v
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &bodies
}

func do(t *testing.T, c *http.Client, method, url, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestRetryIdempotentOn5xx(t *testing.T) {
	c, waits := newClient(gitoperationslib.DefaultRetryPolicy)
	srv, n, bodies := script(t, []int{503, 502, 200}, nil, "")
	resp := do(t, c, http.MethodPut, srv.URL, `{"v":1}`)
	if resp.StatusCode != 200 || n.Load() != 3 {
		t.Fatalf("status %d after %d requests, want 200 after 3", resp.StatusCode, n.Load())
	}
	for i, b := range *bodies {
		if b != `{"v":1}` {
			t.Errorf("request %d body = %q, want it replayed", i+1, b)
		}
	}
	// Backoff: ~1 s then ~2 s (half of it jitter).
	if len(*waits) != 2 || (*waits)[0] < 500*time.Millisecond || (*waits)[0] > time.Second ||
		(*waits)[1] < time.Second || (*waits)[1] > 2*time.Second {
		t.Errorf("waits = %v", *waits)
	}
}

func TestNoRetryOfPostOn5xx(t *testing.T) {
	c, _ := newClient(gitoperationslib.DefaultRetryPolicy)
	srv, n, _ := script(t, []int{503, 200}, nil, "")
	if resp := do(t, c, http.MethodPost, srv.URL, "x"); resp.StatusCode != 503 || n.Load() != 1 {
		t.Fatalf("status %d after %d requests, want 503 after 1", resp.StatusCode, n.Load())
	}
}

func TestGiveUpAfterMaxAttempts(t *testing.T) {
	c, waits := newClient(gitoperationslib.RetryPolicy{MaxAttempts: 2, MaxWait: time.Minute})
	srv, n, _ := script(t, []int{504}, nil, "gateway")
	resp := do(t, c, http.MethodGet, srv.URL, "")
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 504 || n.Load() != 2 || len(*waits) != 1 || string(data) != "gateway" {
		t.Fatalf("status %d, %d requests, waits %v, body %q", resp.StatusCode, n.Load(), *waits, data)
	}
}

func TestRetryAfterHonoured(t *testing.T) {
	c, waits := newClient(gitoperationslib.DefaultRetryPolicy)
	srv, n, _ := script(t, []int{429, 200}, http.Header{"Retry-After": {"7"}}, "")
	// 429 is retried whatever the method: the server did not process it.
	if resp := do(t, c, http.MethodPost, srv.URL, "x"); resp.StatusCode != 200 || n.Load() != 2 {
		t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
	}
	if len(*waits) != 1 || (*waits)[0] != 7*time.Second {
		t.Errorf("waits = %v, want [7s]", *waits)
	}
}

func TestRetryAfterBeyondMaxWait(t *testing.T) {
	c, waits := newClient(gitoperationslib.DefaultRetryPolicy)
	srv, n, _ := script(t, []int{503, 200}, http.Header{"Retry-After": {"600"}}, "")
	if resp := do(t, c, http.MethodGet, srv.URL, ""); resp.StatusCode != 503 || n.Load() != 1 || len(*waits) != 0 {
		t.Fatalf("status %d after %d requests, waits %v: want an immediate 503", resp.StatusCode, n.Load(), *waits)
	}
}

func TestForbidden(t *testing.T) {
	t.Run("permission refusal", func(t *testing.T) {
		c, _ := newClient(gitoperationslib.DefaultRetryPolicy)
		srv, n, _ := script(t, []int{403, 200}, nil, "")
		if resp := do(t, c, http.MethodGet, srv.URL, ""); resp.StatusCode != 403 || n.Load() != 1 {
			t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		c, waits := newClient(gitoperationslib.DefaultRetryPolicy)
		reset := fakeNow.Add(10 * time.Second).Unix()
		h := http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {itoa(reset)}}
		srv, n, _ := script(t, []int{403, 200}, h, "")
		if resp := do(t, c, http.MethodGet, srv.URL, ""); resp.StatusCode != 200 || n.Load() != 2 {
			t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
		}
		if len(*waits) != 1 || (*waits)[0] != 10*time.Second {
			t.Errorf("waits = %v, want [10s]", *waits)
		}
	})
}

func TestConflict(t *testing.T) {
	const msg = `{"message":"Conflicting repository operation is still in progress"}`
	t.Run("in progress", func(t *testing.T) {
		c, _ := newClient(gitoperationslib.DefaultRetryPolicy, "conflicting repository operation is still in progress")
		srv, n, _ := script(t, []int{409, 200}, nil, msg)
		if resp := do(t, c, http.MethodPatch, srv.URL, "x"); resp.StatusCode != 200 || n.Load() != 2 {
			t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
		}
	})
	t.Run("other conflict", func(t *testing.T) {
		c, _ := newClient(gitoperationslib.DefaultRetryPolicy, "conflicting repository operation is still in progress")
		srv, n, _ := script(t, []int{409, 200}, nil, `{"message":"Git Repository is empty."}`)
		resp := do(t, c, http.MethodGet, srv.URL, "")
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 409 || n.Load() != 1 || !strings.Contains(string(data), "is empty") {
			t.Fatalf("status %d after %d requests, body %q: want the 409 passed through intact", resp.StatusCode, n.Load(), data)
		}
	})
	t.Run("no conflicts configured", func(t *testing.T) {
		c, _ := newClient(gitoperationslib.DefaultRetryPolicy)
		srv, n, _ := script(t, []int{409, 200}, nil, msg)
		if resp := do(t, c, http.MethodGet, srv.URL, ""); resp.StatusCode != 409 || n.Load() != 1 {
			t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
		}
	})
}

func TestStreamedBodyNotRetried(t *testing.T) {
	c, _ := newClient(gitoperationslib.DefaultRetryPolicy)
	srv, n, _ := script(t, []int{503, 200}, nil, "")
	// An io.Pipe has no GetBody: it cannot be replayed.
	pr, pw := io.Pipe()
	go func() { _, _ = io.WriteString(pw, "stream"); pw.Close() }()
	req, _ := http.NewRequest(http.MethodPut, srv.URL, pr)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 || n.Load() != 1 {
		t.Fatalf("status %d after %d requests", resp.StatusCode, n.Load())
	}
}

func TestDroppedConnection(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close() // no response at all
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newClient(gitoperationslib.DefaultRetryPolicy)
	if resp := do(t, c, http.MethodGet, srv.URL, ""); resp.StatusCode != 200 || n.Load() != 2 {
		t.Fatalf("GET: status %d after %d requests", resp.StatusCode, n.Load())
	}

	n.Store(0)
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("x"))
	if _, err := c.Do(req); err == nil || n.Load() != 1 {
		t.Fatalf("POST: err = %v after %d requests, want the error after 1", err, n.Load())
	}
}

func TestRefusedConnectionFailsFast(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	c, waits := newClient(gitoperationslib.DefaultRetryPolicy)
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr, nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("request to a closed port succeeded")
	}
	if len(*waits) != 0 {
		t.Errorf("waits = %v, want none", *waits)
	}
}

func TestContextCancelledDuringWait(t *testing.T) {
	srv, n, _ := script(t, []int{503}, nil, "")
	c := Wrap(nil, gitoperationslib.RetryPolicy{MaxAttempts: 5, MaxWait: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	start := time.Now()
	if _, err := c.Do(req); err == nil {
		t.Fatal("want the context error")
	}
	if time.Since(start) > 2*time.Second || n.Load() != 1 {
		t.Errorf("took %s and %d requests: the wait should end with the context", time.Since(start), n.Load())
	}
}

func TestWrap(t *testing.T) {
	orig := &http.Client{Timeout: time.Minute}
	if got := Wrap(orig, gitoperationslib.NoRetry); got != orig {
		t.Error("NoRetry: want the client unchanged")
	}
	got := Wrap(orig, gitoperationslib.DefaultRetryPolicy)
	if got == orig || orig.Transport != nil || got.Timeout != time.Minute {
		t.Errorf("Wrap modified the caller's client or lost its settings: %+v", got)
	}
	if Wrap(nil, gitoperationslib.NoRetry) != http.DefaultClient {
		t.Error("nil client: want http.DefaultClient")
	}
}

func TestRetryAt(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		want   time.Time
		ok     bool
	}{
		{"seconds", http.Header{"Retry-After": {"30"}}, fakeNow.Add(30 * time.Second), true},
		{"date", http.Header{"Retry-After": {fakeNow.Add(time.Minute).Format(http.TimeFormat)}}, fakeNow.Add(time.Minute), true},
		{"rate limit", http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {itoa(fakeNow.Unix() + 90)}}, fakeNow.Add(90 * time.Second), true},
		{"limit not exhausted", http.Header{"X-Ratelimit-Remaining": {"12"}, "X-Ratelimit-Reset": {itoa(fakeNow.Unix() + 90)}}, time.Time{}, false},
		{"garbage", http.Header{"Retry-After": {"soon"}}, time.Time{}, false},
		{"none", http.Header{}, time.Time{}, false},
	}
	for _, tc := range cases {
		got, ok := RetryAt(tc.header, fakeNow)
		if ok != tc.ok || !got.Equal(tc.want) {
			t.Errorf("%s: RetryAt = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
