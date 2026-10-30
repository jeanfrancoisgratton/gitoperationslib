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
	var reqBody io.Reader
	contentType := ""
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("forgejo: encode request: %w", err)
		}
		reqBody, contentType = bytes.NewReader(data), "application/json"
	}
	req, err := c.newRequest(ctx, method, c.apiURL(path, query), reqBody, contentType)
	if err != nil {
		return nil, err
	}
	return c.doJSON(req, out)
}

func (c *Client) apiURL(path string, query url.Values) string {
	u := c.baseURL.JoinPath(path)
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *Client) newRequest(ctx context.Context, method, rawURL string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("forgejo: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.authorize != nil {
		c.authorize(req)
	}
	return req, nil
}

// send performs req and turns non-2xx responses into *APIError. On success
// the caller owns the response body.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forgejo: %s %s: %w", req.Method, req.URL.Path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer closeBody(resp)
		return nil, decodeError(resp)
	}
	return resp, nil
}

func (c *Client) doJSON(req *http.Request, out any) (*Response, error) {
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("forgejo: decode response from %s %s: %w", req.Method, req.URL.Path, err)
		}
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		NextPage:   nextPage(resp.Header),
		TotalCount: totalCount(resp.Header),
	}, nil
}

// Text GETs path (relative to /api/v1, already escaped) and returns the
// response body, for the few endpoints that answer text/plain.
func (c *Client) Text(ctx context.Context, path string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.apiURL(path, nil), nil, "")
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := c.send(req)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return "", fmt.Errorf("forgejo: read response from GET %s: %w", req.URL.Path, err)
	}
	return string(data), nil
}

// closeBody drains a little of the body, so the connection can be reused,
// and closes it.
func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	resp.Body.Close()
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

// BaseURL returns the API base URL, ending in "/api/v1/".
func (c *Client) BaseURL() string { return c.baseURL.String() }
