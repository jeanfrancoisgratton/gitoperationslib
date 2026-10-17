// Package forgejo is the Forgejo backend.
package forgejo

import (
	"context"
	"net/http"
	"sync"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
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

// BasicAuth authenticates with a username and password.
func BasicAuth(username, password string) Credentials { return basicAuth{username, password} }

// Option configures New.
type Option func(*config)

type config struct {
	httpClient *http.Client
	userAgent  string
}

// WithHTTPClient sets the HTTP client, e.g. for timeouts, proxies or a custom
// CA. The default is http.DefaultClient.
func WithHTTPClient(c *http.Client) Option { return func(cfg *config) { cfg.httpClient = c } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(cfg *config) { cfg.userAgent = ua } }

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
	cfg := config{userAgent: "gitoperationslib"}
	for _, opt := range opts {
		opt(&cfg)
	}
	var authorize func(*http.Request)
	if creds != nil {
		authorize = creds.apply
	}
	api, err := forgejoapi.NewClient(baseURL, cfg.httpClient, authorize, cfg.userAgent)
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
	return gitoperationslib.CapRepos | gitoperationslib.CapAdmin | gitoperationslib.CapReleases | gitoperationslib.CapExternalAssets | gitoperationslib.CapPullRequests
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
