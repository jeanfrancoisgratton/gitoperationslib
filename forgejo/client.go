// Package forgejo is the Forgejo backend.
package forgejo

import (
	"context"
	"net/http"

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
}

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
