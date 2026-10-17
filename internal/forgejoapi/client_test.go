package forgejoapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://git.example.com", "https://git.example.com/api/v1/"},
		{"https://git.example.com/", "https://git.example.com/api/v1/"},
		{"https://git.example.com:3000/api/v1", "https://git.example.com:3000/api/v1/"},
		{"https://git.example.com/api/v1/", "https://git.example.com/api/v1/"},
		{" https://example.com/forgejo/?x=1#f ", "https://example.com/forgejo/api/v1/"},
	}
	for _, tt := range tests {
		u, err := normalizeBaseURL(tt.in)
		if err != nil {
			t.Errorf("normalizeBaseURL(%q): %v", tt.in, err)
			continue
		}
		if u.String() != tt.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.in, u, tt.want)
		}
	}
	for _, bad := range []string{"", "git.example.com", "ftp://git.example.com", "https://"} {
		if _, err := normalizeBaseURL(bad); err == nil {
			t.Errorf("normalizeBaseURL(%q) succeeded", bad)
		}
	}
}

func TestPath(t *testing.T) {
	if got, want := Path("repos", "my org", "a/b"), "repos/my%20org/a%2Fb"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, srv.Client(), func(r *http.Request) { r.Header.Set("Authorization", "token secret") }, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDoRoundTrip(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/v1/repos/my%20org/x" {
			t.Errorf("got %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		for h, want := range map[string]string{
			"Authorization": "token secret",
			"User-Agent":    "test-agent",
			"Accept":        "application/json",
			"Content-Type":  "application/json",
		} {
			if got := r.Header.Get(h); got != want {
				t.Errorf("%s = %q, want %q", h, got, want)
			}
		}
		var in map[string]string
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in["name"] != "x" {
			t.Errorf("body = %v, %v", in, err)
		}
		w.Header().Set("Link", `<http://h/api/v1/x?page=3&limit=1>; rel="next", <http://h/api/v1/x?page=9&limit=1>; rel="last"`)
		w.Header().Set("X-Total-Count", "9")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":42}`)
	})

	var out struct{ ID int }
	resp, err := c.Do(context.Background(), http.MethodPost, Path("repos", "my org", "x"),
		url.Values{"page": {"2"}}, map[string]string{"name": "x"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 42 || resp.StatusCode != 201 || resp.NextPage != 3 || resp.TotalCount != 9 {
		t.Errorf("out=%+v resp=%+v", out, resp)
	}
}

func TestDoNoBodyNoContent(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" {
			t.Error("Content-Type set without body")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	var out struct{}
	resp, err := c.Do(context.Background(), http.MethodDelete, "repos/a/b", nil, nil, &out)
	if err != nil || resp.TotalCount != -1 || resp.NextPage != 0 {
		t.Errorf("resp=%+v err=%v", resp, err)
	}
}

func TestDoErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
		wantDocURL  string
		wantDetails []string
		wantString  string
	}{
		{
			name:        "forgejo json",
			status:      404,
			body:        `{"message":"GetUserByName","url":"https://h/api/swagger","errors":["user redirect does not exist [name: x]"]}`,
			wantMessage: "GetUserByName",
			wantDocURL:  "https://h/api/swagger",
			wantDetails: []string{"user redirect does not exist [name: x]"},
			wantString:  "forgejo: HTTP 404: GetUserByName [user redirect does not exist [name: x]]",
		},
		{
			name:        "non-string errors",
			status:      422,
			body:        `{"message":"invalid","errors":[{"field":"name"}]}`,
			wantMessage: "invalid",
			wantDetails: []string{`{"field":"name"}`},
		},
		{name: "html", status: 502, body: "<html>Bad Gateway</html>", wantString: "forgejo: HTTP 502: Bad Gateway"},
		{name: "empty", status: 401, body: "", wantString: "forgejo: HTTP 401: Unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			_, err := c.Do(context.Background(), http.MethodGet, "x", nil, nil, nil)
			var apiErr *gitoperationslib.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Backend != gitoperationslib.BackendForgejo || apiErr.StatusCode != tt.status ||
				apiErr.Message != tt.wantMessage || apiErr.DocumentationURL != tt.wantDocURL ||
				len(apiErr.Details) != len(tt.wantDetails) {
				t.Fatalf("got %+v", apiErr)
			}
			for i := range tt.wantDetails {
				if apiErr.Details[i] != tt.wantDetails[i] {
					t.Errorf("Details[%d] = %q", i, apiErr.Details[i])
				}
			}
			if tt.wantString != "" && err.Error() != tt.wantString {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantString)
			}
		})
	}
}

func TestDoBadJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{not json") })
	var out map[string]any
	if _, err := c.Do(context.Background(), http.MethodGet, "x", nil, nil, &out); err == nil {
		t.Error("expected decode error")
	}
}

func TestDoContextCanceled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request should not be sent") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Do(ctx, http.MethodGet, "x", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestVersion(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/version" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"version":"15.0.2+gitea-1.22.0"}`)
	})
	v, err := c.Version(context.Background())
	if err != nil || v != "15.0.2+gitea-1.22.0" {
		t.Errorf("Version() = %q, %v", v, err)
	}
}
