// Package bitbucket is the Bitbucket Cloud backend (REST API 2.0).
//
// Repository owners are workspaces. Bitbucket Cloud has no releases, no tag
// protection, and its issue tracker (with labels and milestones) has been
// retired: those areas return ErrNotSupported. User and workspace
// administration is not implemented.
package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

// Credentials authenticate requests. Create them with Token or BasicAuth.
type Credentials interface {
	apply(*http.Request)
}

type tokenAuth string

func (t tokenAuth) apply(r *http.Request) { r.Header.Set("Authorization", "Bearer "+string(t)) }

// Token authenticates with an access token sent as a bearer token: an
// Atlassian API token with Bitbucket scopes, or a workspace, project or
// repository access token.
func Token(token string) Credentials { return tokenAuth(token) }

type basicAuth struct{ username, password string }

func (b basicAuth) apply(r *http.Request) { r.SetBasicAuth(b.username, b.password) }

// BasicAuth authenticates with a username (or Atlassian account e-mail) and
// an API token or app password.
func BasicAuth(username, password string) Credentials { return basicAuth{username, password} }

// Option configures New.
type Option func(*config)

type config struct {
	httpClient *http.Client
	userAgent  string
	baseURL    string
}

// WithHTTPClient sets the HTTP client (timeouts, proxies).
func WithHTTPClient(c *http.Client) Option { return func(cfg *config) { cfg.httpClient = c } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(cfg *config) { cfg.userAgent = ua } }

// WithBaseURL overrides the API root (default https://api.bitbucket.org/2.0/).
func WithBaseURL(u string) Option { return func(cfg *config) { cfg.baseURL = u } }

// Client is a Bitbucket Cloud backend client.
type Client struct {
	api *bitbucketapi.Client

	mu sync.Mutex
	me *userJSON // authenticated user, cached by currentUser
}

var _ gitoperationslib.Client = (*Client)(nil)

// New returns a Bitbucket Cloud client. No request is made. Nil creds means
// anonymous access.
func New(creds Credentials, opts ...Option) (*Client, error) {
	cfg := config{userAgent: "gitoperationslib"}
	for _, opt := range opts {
		opt(&cfg)
	}
	var authorize func(*http.Request)
	if creds != nil {
		authorize = creds.apply
	}
	api, err := bitbucketapi.NewClient(cfg.baseURL, cfg.httpClient, authorize, cfg.userAgent)
	if err != nil {
		return nil, err
	}
	return &Client{api: api}, nil
}

func (c *Client) Backend() gitoperationslib.Backend { return gitoperationslib.BackendBitbucket }

func (c *Client) Capabilities() gitoperationslib.Capability {
	return gitoperationslib.CapRepos | gitoperationslib.CapBranches | gitoperationslib.CapTags |
		gitoperationslib.CapBranchProtection | gitoperationslib.CapPullRequests | gitoperationslib.CapWebhooks
}

// userJSON is a Bitbucket account. There is no public login name: Nickname is
// used where other backends report a login.
type userJSON struct {
	UUID        string `json:"uuid"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"` // only for the authenticated user
}

func (u *userJSON) login() string {
	switch {
	case u == nil:
		return ""
	case u.Nickname != "":
		return u.Nickname
	}
	return u.DisplayName
}

// currentUser returns the authenticated user, fetching it once.
func (c *Client) currentUser(ctx context.Context) (*userJSON, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.me != nil {
		return c.me, nil
	}
	var u userJSON
	if _, err := c.api.Do(ctx, http.MethodGet, "user", nil, nil, &u); err != nil {
		return nil, err
	}
	c.me = &u
	return c.me, nil
}

// workspaces lists the slugs of the workspaces the user belongs to.
func (c *Client) workspaces(ctx context.Context) ([]string, error) {
	type membership struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	all, err := bitbucketapi.All[membership](ctx, c.api, "user/workspaces", nil)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, len(all))
	for i, m := range all {
		slugs[i] = m.Workspace.Slug
	}
	return slugs, nil
}

// defaultWorkspace returns the only workspace the user belongs to; with
// several, the caller must name one.
func (c *Client) defaultWorkspace(ctx context.Context) (string, error) {
	slugs, err := c.workspaces(ctx)
	if err != nil {
		return "", err
	}
	if len(slugs) != 1 {
		return "", fmt.Errorf("bitbucket: %d workspaces available; name one as the owner", len(slugs))
	}
	return slugs[0], nil
}

// resolveUser turns a nickname, account ID or {UUID} into a user UUID, looking
// nicknames up among the workspace's members.
func (c *Client) resolveUser(ctx context.Context, workspace, who string) (string, error) {
	if strings.HasPrefix(who, "{") {
		return who, nil
	}
	type member struct {
		User userJSON `json:"user"`
	}
	all, err := bitbucketapi.All[member](ctx, c.api, bitbucketapi.Path("workspaces", workspace, "members"), nil)
	if err != nil {
		return "", err
	}
	for _, m := range all {
		if m.User.AccountID == who || strings.EqualFold(m.User.Nickname, who) || strings.EqualFold(m.User.DisplayName, who) {
			return m.User.UUID, nil
		}
	}
	return "", &gitoperationslib.APIError{
		Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("no member %q in workspace %s", who, workspace),
	}
}

func notSupported(op, reason string) error {
	return gitoperationslib.NotSupported(gitoperationslib.BackendBitbucket, op, reason)
}

var errNoName = errors.New("bitbucket: name is required")
