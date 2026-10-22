// Package gitoperationslib wraps git forge operations behind a common set of
// interfaces. Forgejo is the primary backend; GitHub and Bitbucket Cloud are
// supported where their APIs overlap. Operations a backend cannot perform
// return an error matching ErrNotSupported.
//
// Create a client with forgejo.New, github.New or bitbucket.New; the webhook
// sub-package receives and verifies deliveries. See docs/USAGE.md in the
// repository for the full guide.
package gitoperationslib
