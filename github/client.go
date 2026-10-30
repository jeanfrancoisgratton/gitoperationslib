// Package github is the GitHub backend, built on google/go-github.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/retry"
)

// Option configures New.
type Option func(*config)

type config struct {
	httpClient           *http.Client
	userAgent            string
	retry                gitoperationslib.RetryPolicy
	baseURL, uploadURL   string
	enterpriseConfigured bool
}

// WithHTTPClient sets the HTTP client, e.g. for timeouts or proxies.
func WithHTTPClient(c *http.Client) Option { return func(cfg *config) { cfg.httpClient = c } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(cfg *config) { cfg.userAgent = ua } }

// WithRetry sets the retry policy; the default is
// gitoperationslib.DefaultRetryPolicy, and gitoperationslib.NoRetry disables
// retries. GitHub's 409 "repository operation still in progress", returned
// for a while after creating a repository with AutoInit, is retried too.
func WithRetry(p gitoperationslib.RetryPolicy) Option { return func(cfg *config) { cfg.retry = p } }

// conflictInProgress is GitHub's 409 message for an operation that has to
// wait for a previous one on the same repository.
const conflictInProgress = "conflicting repository operation is still in progress"

// WithEnterpriseURL targets a GitHub Enterprise Server instance. "/api/v3/"
// (and "/api/uploads/" for uploadURL) is appended when missing; an empty
// uploadURL means baseURL.
func WithEnterpriseURL(baseURL, uploadURL string) Option {
	return func(cfg *config) {
		cfg.baseURL, cfg.uploadURL, cfg.enterpriseConfigured = baseURL, uploadURL, true
	}
}

// Client is a GitHub backend client.
type Client struct {
	gh *gh.Client
	// http is the caller's plain client (no credentials attached). It
	// follows asset redirects to the storage host and carries GraphQL
	// requests, which add the token themselves.
	http      *http.Client
	token     string
	userAgent string

	mu    sync.Mutex
	login string // authenticated user, cached by currentLogin
}

var _ gitoperationslib.Client = (*Client)(nil)

// New returns a client for github.com (or GitHub Enterprise Server with
// WithEnterpriseURL), authenticated with a personal access token. An empty
// token means anonymous access. No request is made.
func New(token string, opts ...Option) (*Client, error) {
	cfg := config{userAgent: "gitoperationslib", retry: gitoperationslib.DefaultRetryPolicy}
	for _, opt := range opts {
		opt(&cfg)
	}
	plain := retry.Wrap(cfg.httpClient, cfg.retry, conflictInProgress)
	ghOpts := []gh.ClientOptionsFunc{gh.WithUserAgent(cfg.userAgent), gh.WithHTTPClient(plain)}
	if token != "" {
		ghOpts = append(ghOpts, gh.WithAuthToken(token))
	}
	if cfg.enterpriseConfigured {
		upload := cfg.uploadURL
		if upload == "" {
			upload = cfg.baseURL
		}
		ghOpts = append(ghOpts, gh.WithEnterpriseURLs(cfg.baseURL, upload))
	}
	c, err := gh.NewClient(ghOpts...)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	return &Client{gh: c, http: plain, token: token, userAgent: cfg.userAgent}, nil
}

func (c *Client) Backend() gitoperationslib.Backend { return gitoperationslib.BackendGitHub }

func (c *Client) Capabilities() gitoperationslib.Capability {
	return gitoperationslib.CapRepos | gitoperationslib.CapReleases | gitoperationslib.CapPullRequests | gitoperationslib.CapWebhooks |
		gitoperationslib.CapBranches | gitoperationslib.CapTags | gitoperationslib.CapBranchProtection | gitoperationslib.CapTagProtection | gitoperationslib.CapIssues |
		gitoperationslib.CapDirectory | gitoperationslib.CapAccount | gitoperationslib.CapSecrets
}

func (c *Client) Repos() gitoperationslib.Repos { return repos{c} }

// currentLogin returns the authenticated user's login, fetching it once.
func (c *Client) currentLogin(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.login != "" {
		return c.login, nil
	}
	u, _, err := c.gh.Users.Get(ctx, "")
	if err != nil {
		return "", convertError(err)
	}
	c.login = u.GetLogin()
	return c.login, nil
}

// isOrg reports whether name is an organisation (as opposed to a user).
func (c *Client) isOrg(ctx context.Context, name string) (bool, error) {
	_, _, err := c.gh.Organizations.Get(ctx, name)
	if err == nil {
		return true, nil
	}
	err = convertError(err)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// convertError turns go-github API errors into *gitoperationslib.APIError.
// Other errors (network, context) are returned unchanged.
func convertError(err error) error {
	if err == nil {
		return nil
	}
	apiErr := &gitoperationslib.APIError{Backend: gitoperationslib.BackendGitHub}

	var rateErr *gh.RateLimitError
	var abuseErr *gh.AbuseRateLimitError
	var respErr *gh.ErrorResponse
	switch {
	case errors.As(err, &rateErr):
		apiErr.StatusCode = statusOf(rateErr.Response)
		apiErr.Message = rateErr.Message
		apiErr.Details = []string{fmt.Sprintf("rate limit resets at %s", rateErr.Rate.Reset.Time.UTC().Format("2006-01-02T15:04:05Z"))}
		apiErr.RetryAt = rateErr.Rate.Reset.Time
	case errors.As(err, &abuseErr):
		apiErr.StatusCode = statusOf(abuseErr.Response)
		apiErr.Message = abuseErr.Message
		if abuseErr.RetryAfter != nil {
			apiErr.Details = []string{fmt.Sprintf("retry after %s", *abuseErr.RetryAfter)}
			apiErr.RetryAt = time.Now().Add(*abuseErr.RetryAfter)
		}
	case errors.As(err, &respErr):
		apiErr.StatusCode = statusOf(respErr.Response)
		apiErr.Message = respErr.Message
		apiErr.DocumentationURL = respErr.DocumentationURL
		if respErr.Response != nil {
			apiErr.RetryAt, _ = retry.RetryAt(respErr.Response.Header, time.Now())
		}
		for _, e := range respErr.Errors {
			apiErr.Details = append(apiErr.Details, errorDetail(e))
		}
	default:
		return err
	}
	return apiErr
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func errorDetail(e gh.Error) string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%s.%s: %s", e.Resource, e.Field, e.Code)
}
