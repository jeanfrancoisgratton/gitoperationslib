// Package forgejo is the Forgejo backend.
package forgejo

import (
	"context"
	"net/http"
	"sync"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/retry"
)

// Credentials authenticate requests. Create them with Token or BasicAuth.
type Credentials interface {
	apply(*http.Request)
}

type tokenAuth string

func (t tokenAuth) apply(r *http.Request) { r.Header.Set("Authorization", "token "+string(t)) }

// Token authenticates with a personal access token.
func Token(token string) Credentials { return tokenAuth(token) }

type basicAuth struct{ username, password string }

func (b basicAuth) apply(r *http.Request) { r.SetBasicAuth(b.username, b.password) }

// BasicAuth authenticates with a username and password. For an account with
// two-factor authentication, every request (New's version check included)
// fails with an error matching gitoperationslib.ErrInvalidOTP: use
// BasicAuthOTP.
func BasicAuth(username, password string) Credentials { return basicAuth{username, password} }

type basicAuthOTP struct{ username, password, otp string }

func (b basicAuthOTP) apply(r *http.Request) {
	r.SetBasicAuth(b.username, b.password)
	r.Header.Set("X-FORGEJO-OTP", b.otp)
}

// BasicAuthOTP authenticates an account with two-factor authentication: a
// username, password and the current TOTP code, sent with every request
// (X-FORGEJO-OTP). Forgejo accepts the same code for any number of requests
// while it is valid: until the end of the 30-second period after the one it
// was generated in, so 30 to 60 seconds (checked on Forgejo 15.0.11). After
// that, requests fail with an error matching gitoperationslib.ErrInvalidOTP.
// For anything that may take longer, use BasicAuthOTPFunc.
//
// Token management (Account().Tokens()) needs this, or BasicAuth: Forgejo
// refuses token authentication there.
func BasicAuthOTP(username, password, otp string) Credentials {
	return basicAuthOTP{username, password, otp}
}

type basicAuthOTPFunc struct {
	username, password string
	otp                func() string
}

func (b basicAuthOTPFunc) apply(r *http.Request) {
	r.SetBasicAuth(b.username, b.password)
	r.Header.Set("X-FORGEJO-OTP", b.otp())
}

// BasicAuthOTPFunc is BasicAuthOTP for work that may outlast a code: otp is
// called before every request, and returns the code to send. It is typically
// computed from the account's TOTP secret, or a prompted code cached until
// its period ends. otp may be called from several goroutines at once.
func BasicAuthOTPFunc(username, password string, otp func() string) Credentials {
	return basicAuthOTPFunc{username, password, otp}
}

// Option configures New.
type Option func(*config)

type config struct {
	httpClient *http.Client
	userAgent  string
	retry      gitoperationslib.RetryPolicy
	sudo       string
}

// WithHTTPClient sets the HTTP client, e.g. for timeouts, proxies or a custom
// CA. The default is http.DefaultClient.
func WithHTTPClient(c *http.Client) Option { return func(cfg *config) { cfg.httpClient = c } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(cfg *config) { cfg.userAgent = ua } }

// WithRetry sets the retry policy; the default is
// gitoperationslib.DefaultRetryPolicy, and gitoperationslib.NoRetry disables
// retries.
func WithRetry(p gitoperationslib.RetryPolicy) Option { return func(cfg *config) { cfg.retry = p } }

// WithSudo makes every request act as the user login (Sudo header): the
// server applies that user's permissions and attributes changes to them, and
// Directory().Me() returns them. The credentials must be a site
// administrator's; other users get HTTP 403 "Only administrators allowed to
// sudo", and an unknown login HTTP 404 on every request, New's version check
// included.
func WithSudo(login string) Option { return func(cfg *config) { cfg.sudo = login } }

// Client is a Forgejo backend client.
type Client struct {
	api     *forgejoapi.Client
	version Version

	mu    sync.Mutex
	login string // authenticated user, cached by currentLogin
}

var (
	_ gitoperationslib.Client = (*Client)(nil)
	_ gitoperationslib.Admin  = (*Client)(nil)
)

// New connects to the Forgejo instance at baseURL (the instance root, or its
// /api/v1 endpoint) and checks that its version is at least MinimumVersion,
// returning *gitoperationslib.UnsupportedVersionError otherwise.
// Nil creds means anonymous access.
func New(ctx context.Context, baseURL string, creds Credentials, opts ...Option) (*Client, error) {
	cfg := config{userAgent: "gitoperationslib", retry: gitoperationslib.DefaultRetryPolicy}
	for _, opt := range opts {
		opt(&cfg)
	}
	var authorize func(*http.Request)
	if creds != nil {
		authorize = creds.apply
	}
	if cfg.sudo != "" {
		inner := authorize
		authorize = func(r *http.Request) {
			if inner != nil {
				inner(r)
			}
			r.Header.Set("Sudo", cfg.sudo)
		}
	}
	api, err := forgejoapi.NewClient(baseURL, retry.Wrap(cfg.httpClient, cfg.retry), authorize, cfg.userAgent)
	if err != nil {
		return nil, err
	}

	raw, err := api.Version(ctx)
	if err != nil {
		return nil, err
	}
	v, err := ParseVersion(raw)
	if err != nil {
		return nil, err
	}
	if v.Compare(MinimumVersion) < 0 {
		return nil, &gitoperationslib.UnsupportedVersionError{
			Backend: gitoperationslib.BackendForgejo,
			Version: raw,
			Minimum: MinimumVersion.String(),
		}
	}
	return &Client{api: api, version: v}, nil
}

// Version returns the server version detected by New.
func (c *Client) Version() Version { return c.version }

func (c *Client) Backend() gitoperationslib.Backend { return gitoperationslib.BackendForgejo }

func (c *Client) Capabilities() gitoperationslib.Capability {
	return gitoperationslib.CapRepos | gitoperationslib.CapAdmin | gitoperationslib.CapReleases | gitoperationslib.CapExternalAssets | gitoperationslib.CapPullRequests |
		gitoperationslib.CapWebhooks | gitoperationslib.CapBranches | gitoperationslib.CapTags |
		gitoperationslib.CapBranchProtection | gitoperationslib.CapTagProtection | gitoperationslib.CapIssues |
		gitoperationslib.CapDirectory | gitoperationslib.CapAccount
}

func (c *Client) Repos() gitoperationslib.Repos { return repos{c} }

// currentLogin returns the authenticated user's login, fetching it once.
func (c *Client) currentLogin(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.login != "" {
		return c.login, nil
	}
	var u struct {
		Login string `json:"login"`
	}
	if _, err := c.api.Do(ctx, http.MethodGet, "user", nil, nil, &u); err != nil {
		return "", err
	}
	c.login = u.Login
	return c.login, nil
}

// isOrg reports whether name is an organisation (as opposed to a user).
func (c *Client) isOrg(ctx context.Context, name string) (bool, error) {
	_, err := c.api.Do(ctx, http.MethodGet, forgejoapi.Path("orgs", name), nil, nil, nil)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// toPage converts one page of API items.
func toPage[J, T any](resp *forgejoapi.Response, items []J, convert func(*J) T) gitoperationslib.Page[T] {
	page := gitoperationslib.Page[T]{Items: make([]T, len(items)), NextPage: resp.NextPage}
	for i := range items {
		page.Items[i] = convert(&items[i])
	}
	return page
}
