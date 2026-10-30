// Package retry is the http.RoundTripper every backend wraps around its HTTP
// client to retry transient failures (see gitoperationslib.RetryPolicy).
package retry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// baseDelay is the first backoff delay; it doubles on every retry.
const baseDelay = time.Second

// maxSniff caps how much of a 409 response is read to match Conflicts.
const maxSniff = 64 << 10

// Transport retries requests according to Policy.
type Transport struct {
	Base   http.RoundTripper // nil means http.DefaultTransport
	Policy gitoperationslib.RetryPolicy
	// Conflicts are fragments of 409 responses that mean "not processed, try
	// again" (GitHub: an operation on the repository is still in progress).
	// They are matched case-insensitively against the response body.
	Conflicts []string

	sleep func(context.Context, time.Duration) error // nil means sleepCtx; replaced by tests
	now   func() time.Time                           // nil means time.Now; replaced by tests
}

// Wrap returns a copy of c whose transport retries according to p; c itself
// is not modified. A nil c stands for http.DefaultClient. When p disables
// retries, c is returned as is.
func Wrap(c *http.Client, p gitoperationslib.RetryPolicy, conflicts ...string) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	if p.MaxAttempts <= 1 {
		return c
	}
	cp := *c
	cp.Transport = &Transport{Base: c.Transport, Policy: p, Conflicts: conflicts}
	return &cp
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	// A body that cannot be replayed (a streamed upload) is sent once.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return base.RoundTrip(req)
	}
	for attempt := 1; ; attempt++ {
		r := req
		if attempt > 1 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r = req.Clone(req.Context())
			r.Body = body
		}
		resp, err := base.RoundTrip(r)
		if attempt >= t.Policy.MaxAttempts || req.Context().Err() != nil {
			return resp, err
		}
		wait, retry := t.decide(req, resp, err, attempt)
		if !retry {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSniff))
			resp.Body.Close()
		}
		if err := t.wait(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// decide reports whether a failed attempt is retried, and after how long.
func (t *Transport) decide(req *http.Request, resp *http.Response, err error, attempt int) (time.Duration, bool) {
	idempotent := isIdempotent(req.Method)
	if err != nil {
		return t.backoff(attempt), idempotent && isTransient(err)
	}
	switch code := resp.StatusCode; {
	case code == http.StatusTooManyRequests:
		return t.serverWait(resp, attempt)
	case code == http.StatusForbidden:
		// A rate limit (GitHub) rather than a permission refusal.
		if !RateLimited(resp.Header) {
			return 0, false
		}
		return t.serverWait(resp, attempt)
	case code == http.StatusConflict:
		return t.backoff(attempt), t.isConflict(resp)
	case code == http.StatusBadGateway, code == http.StatusServiceUnavailable, code == http.StatusGatewayTimeout:
		if !idempotent {
			return 0, false
		}
		return t.serverWait(resp, attempt)
	}
	return 0, false
}

// serverWait honours a wait the server asked for, within MaxWait, and falls
// back to the backoff delay when it asked for none.
func (t *Transport) serverWait(resp *http.Response, attempt int) (time.Duration, bool) {
	at, ok := RetryAt(resp.Header, t.clock())
	if !ok {
		return t.backoff(attempt), true
	}
	d := max(at.Sub(t.clock()), 0)
	return d, d <= t.Policy.MaxWait
}

// isConflict reports whether a 409 matches Conflicts. The body is read and
// put back, so a response that is not retried reaches the caller intact.
func (t *Transport) isConflict(resp *http.Response) bool {
	if len(t.Conflicts) == 0 {
		return false
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxSniff))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(data), resp.Body), resp.Body}
	body := strings.ToLower(string(data))
	for _, c := range t.Conflicts {
		if strings.Contains(body, strings.ToLower(c)) {
			return true
		}
	}
	return false
}

// backoff is baseDelay doubled on every attempt, with jitter (half of it
// random), capped at MaxWait.
func (t *Transport) backoff(attempt int) time.Duration {
	d := baseDelay << (attempt - 1)
	if t.Policy.MaxWait > 0 {
		d = min(d, t.Policy.MaxWait)
	}
	return d/2 + rand.N(d/2+1)
}

func (t *Transport) wait(ctx context.Context, d time.Duration) error {
	if t.sleep != nil {
		return t.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t *Transport) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

// isTransient reports whether a transport error may succeed on a second
// try: the connection was dropped mid-request. Refused connections, DNS
// failures and timeouts are not, so an unreachable server fails fast.
func isTransient(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// RateLimited reports whether a response announces an exhausted rate limit:
// a Retry-After header, or GitHub's X-RateLimit-Remaining at 0.
func RateLimited(h http.Header) bool {
	return h.Get("Retry-After") != "" || h.Get("X-RateLimit-Remaining") == "0"
}

// RetryAt returns when the server allows the request again: Retry-After
// (seconds or an HTTP date), or else X-RateLimit-Reset (Unix seconds) when
// X-RateLimit-Remaining is 0.
func RetryAt(h http.Header, now time.Time) (time.Time, bool) {
	if v := h.Get("Retry-After"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
			return now.Add(time.Duration(sec) * time.Second), true
		}
		if at, err := http.ParseTime(v); err == nil {
			return at, true
		}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if sec, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return time.Unix(sec, 0), true
		}
	}
	return time.Time{}, false
}
