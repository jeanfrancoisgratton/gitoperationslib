// Package forgejoapi is the HTTP transport for the Forgejo backend.
package forgejoapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client sends requests to a Forgejo instance's /api/v1 endpoints.
type Client struct {
	baseURL   *url.URL // always ends in "/api/v1/"
	http      *http.Client
	authorize func(*http.Request) // nil for anonymous access
	userAgent string
}

// NewClient returns a Client for the instance at baseURL, which may be the
// instance root or its /api/v1 endpoint.
func NewClient(baseURL string, httpClient *http.Client, authorize func(*http.Request), userAgent string) (*Client, error) {
	u, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: u, http: httpClient, authorize: authorize, userAgent: userAgent}, nil
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("forgejo: invalid base URL %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("forgejo: invalid base URL %q: need http(s)://host[:port][/path]", raw)
	}
	p := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/api/v1")
	u.Path = p + "/api/v1/"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u, nil
}

// Path joins segments into a request path, escaping each one.
func Path(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	return strings.Join(escaped, "/")
}

// Response holds the metadata of a successful response.
type Response struct {
	StatusCode int
	Header     http.Header
	// NextPage is taken from the Link header; 0 if there is no next page.
	NextPage int
	// TotalCount is taken from X-Total-Count; -1 if the header is absent.
	TotalCount int
}

// Do sends a request to path (relative to /api/v1, already escaped; see Path).
// A non-nil body is sent as JSON; a non-nil out receives the decoded JSON
// response. Non-2xx responses are returned as *gitoperationslib.APIError.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) (*Response, error) {
	u := c.baseURL.JoinPath(path)
	u.RawQuery = query.Encode()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("forgejo: encode request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("forgejo: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.authorize != nil {
		c.authorize(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forgejo: %s %s: %w", method, u.Path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, decodeError(resp)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("forgejo: decode response from %s %s: %w", method, u.Path, err)
		}
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		NextPage:   nextPage(resp.Header),
		TotalCount: totalCount(resp.Header),
	}, nil
}

// Version returns the raw version string reported by /api/v1/version.
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if _, err := c.Do(ctx, http.MethodGet, "version", nil, nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}
