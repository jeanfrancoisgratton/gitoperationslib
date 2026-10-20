// Package bitbucketapi is the HTTP transport for the Bitbucket Cloud backend
// (REST API 2.0).
package bitbucketapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// DefaultBaseURL is Bitbucket Cloud's API root.
const DefaultBaseURL = "https://api.bitbucket.org/2.0/"

// maxErrorBody caps how much of an error response is read.
const maxErrorBody = 64 << 10

// Client sends requests to the Bitbucket Cloud API.
type Client struct {
	baseURL   *url.URL // ends in "/"
	http      *http.Client
	authorize func(*http.Request) // nil for anonymous access
	userAgent string
}

// NewClient returns a Client for baseURL (DefaultBaseURL when empty).
func NewClient(baseURL string, httpClient *http.Client, authorize func(*http.Request), userAgent string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("bitbucket: invalid base URL %q", baseURL)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: u, http: httpClient, authorize: authorize, userAgent: userAgent}, nil
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
}

// Do sends a request to path (relative to the API root, already escaped; see
// Path). A non-nil body is sent as JSON; a non-nil out receives the decoded
// JSON response. Non-2xx responses are returned as *gitoperationslib.APIError.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) (*Response, error) {
	var reqBody io.Reader
	contentType := ""
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("bitbucket: encode request: %w", err)
		}
		reqBody, contentType = bytes.NewReader(data), "application/json"
	}
	return c.send(ctx, method, path, query, reqBody, contentType, out)
}

// PostForm sends a URL-encoded form (used by the /src commit endpoint).
func (c *Client) PostForm(ctx context.Context, path string, form url.Values, out any) (*Response, error) {
	return c.send(ctx, http.MethodPost, path, nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out)
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) (*Response, error) {
	u := c.baseURL.JoinPath(path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: build request: %w", err)
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
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: %s %s: %w", method, u.Path, err)
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
			return nil, fmt.Errorf("bitbucket: decode response from %s %s: %w", method, u.Path, err)
		}
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header}, nil
}

// decodeError turns a non-2xx response into an *APIError. Bitbucket errors
// look like {"type":"error","error":{"message":…,"detail":…,"fields":{…}}}.
func decodeError(resp *http.Response) error {
	apiErr := &gitoperationslib.APIError{Backend: gitoperationslib.BackendBitbucket, StatusCode: resp.StatusCode}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var body struct {
		Error struct {
			Message string              `json:"message"`
			Detail  string              `json:"detail"`
			Fields  map[string][]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil {
		return apiErr
	}
	apiErr.Message = body.Error.Message
	if body.Error.Detail != "" {
		apiErr.Details = append(apiErr.Details, body.Error.Detail)
	}
	for field, msgs := range body.Error.Fields {
		for _, m := range msgs {
			apiErr.Details = append(apiErr.Details, field+": "+m)
		}
	}
	return apiErr
}

// Page is Bitbucket's paginated envelope.
type Page[T any] struct {
	Values []T    `json:"values"`
	Next   string `json:"next"`
}

// NextPage returns the page number of the next link, or 0 on the last page.
func (p *Page[T]) NextPage() int {
	if p.Next == "" {
		return 0
	}
	u, err := url.Parse(p.Next)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(u.Query().Get("page"))
	return n
}

// ListQuery converts ListOptions into Bitbucket's page/pagelen parameters.
func ListQuery(opts gitoperationslib.ListOptions) url.Values {
	q := url.Values{}
	if opts.Page > 0 {
		q.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.PerPage > 0 {
		q.Set("pagelen", strconv.Itoa(min(opts.PerPage, 100)))
	}
	return q
}

// List fetches one page of path into a Page[T].
func List[T any](ctx context.Context, c *Client, path string, query url.Values) (*Page[T], error) {
	var page Page[T]
	if _, err := c.Do(ctx, http.MethodGet, path, query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// All fetches every page of path.
func All[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("pagelen", "100")
	var all []T
	for page := 1; ; {
		query.Set("page", strconv.Itoa(page))
		p, err := List[T](ctx, c, path, query)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Values...)
		if page = p.NextPage(); page == 0 {
			return all, nil
		}
	}
}
