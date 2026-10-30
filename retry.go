package gitoperationslib

import "time"

// RetryPolicy controls how a backend retries failed requests. Every backend
// takes one through its WithRetry option; without it, DefaultRetryPolicy
// applies.
//
// A request is retried when the server refused it without processing it
// (HTTP 429, a rate limit, GitHub's 409 "repository operation still in
// progress"). Requests that may have been processed are retried only if
// they are idempotent (GET, HEAD, PUT, DELETE): after HTTP 502, 503 or 504,
// or a connection reset mid-request. Refused connections and timeouts are
// not retried, so a server that is down still fails fast. Streamed uploads
// are never retried.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries; 1 or less disables retries.
	MaxAttempts int `json:"maxAttempts"`
	// MaxWait is the longest single wait. When the server asks for a longer
	// one (Retry-After, rate-limit reset), the error is returned at once,
	// with APIError.RetryAt set.
	MaxWait time.Duration `json:"maxWait"`
}

// DefaultRetryPolicy is used when no WithRetry option is given: three
// attempts, with waits of about 1 s and 2 s, and server-requested waits of
// up to 30 s.
var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 3, MaxWait: 30 * time.Second}

// NoRetry disables retries.
var NoRetry = RetryPolicy{MaxAttempts: 1}
