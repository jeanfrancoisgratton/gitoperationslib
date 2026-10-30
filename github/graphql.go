package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/retry"
)

// graphqlURL derives the GraphQL endpoint from the REST base URL:
// https://api.github.com/graphql, or <host>/api/graphql on GitHub
// Enterprise Server (whose REST base is <host>/api/v3/).
func (c *Client) graphqlURL() string {
	u, err := url.Parse(c.gh.BaseURL())
	if err != nil { // go-github validated it already
		return c.gh.BaseURL() + "graphql"
	}
	if strings.EqualFold(u.Host, "api.github.com") {
		u.Path = "/graphql"
	} else {
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/v3") + "/graphql"
	}
	return u.String()
}

type graphqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// graphqlStatus maps GraphQL error types to the HTTP status a REST call
// would have returned.
var graphqlStatus = map[string]int{
	"NOT_FOUND":               http.StatusNotFound,
	"FORBIDDEN":               http.StatusForbidden,
	"UNAUTHORIZED":            http.StatusUnauthorized,
	"RATE_LIMITED":            http.StatusForbidden,
	"UNPROCESSABLE":           http.StatusUnprocessableEntity,
	"INSUFFICIENT_SCOPES":     http.StatusForbidden,
	"MAX_NODE_LIMIT_EXCEEDED": http.StatusUnprocessableEntity,
}

// graphql runs a query or mutation and decodes its data into out. GraphQL
// reports most errors in the body of a 200 response; they are returned as
// *gitoperationslib.APIError like REST errors.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("github: encode GraphQL request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("github: build GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: GraphQL: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("github: read GraphQL response: %w", err)
	}

	var body struct {
		Data    json.RawMessage `json:"data"`
		Errors  []graphqlError  `json:"errors"`
		Message string          `json:"message"` // non-GraphQL errors (401, rate limits)
	}
	_ = json.Unmarshal(data, &body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &gitoperationslib.APIError{Backend: gitoperationslib.BackendGitHub, StatusCode: resp.StatusCode, Message: body.Message}
		if at, ok := retry.RetryAt(resp.Header, time.Now()); ok {
			apiErr.RetryAt = at
			if resp.Header.Get("X-RateLimit-Remaining") == "0" {
				apiErr.Details = []string{"rate limit resets at " + at.UTC().Format("2006-01-02T15:04:05Z")}
			}
		}
		return apiErr
	}
	if len(body.Errors) > 0 {
		status, ok := graphqlStatus[body.Errors[0].Type]
		if !ok {
			status = http.StatusUnprocessableEntity
		}
		apiErr := &gitoperationslib.APIError{Backend: gitoperationslib.BackendGitHub, StatusCode: status, Message: body.Errors[0].Message}
		for _, e := range body.Errors[1:] {
			apiErr.Details = append(apiErr.Details, e.Message)
		}
		return apiErr
	}
	if out != nil {
		if err := json.Unmarshal(body.Data, out); err != nil {
			return fmt.Errorf("github: decode GraphQL data: %w", err)
		}
	}
	return nil
}
