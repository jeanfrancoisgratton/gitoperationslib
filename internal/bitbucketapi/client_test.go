package bitbucketapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/2.0", srv.Client(), func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") }, "ua")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestErrorDecoding(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("User-Agent") != "ua" {
			t.Error("missing headers")
		}
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"message":"Bad request","detail":"nope","fields":{"url":["URL resolution failed"]}}}`)
	})
	_, err := c.Do(context.Background(), http.MethodPost, "repositories/w/r/hooks", nil, map[string]string{"a": "b"}, nil)
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) || apiErr.Backend != gitoperationslib.BackendBitbucket || apiErr.StatusCode != 400 ||
		apiErr.Message != "Bad request" || strings.Join(apiErr.Details, "|") != "nope|url: URL resolution failed" {
		t.Errorf("err = %#v", err)
	}
}

func TestPagination(t *testing.T) {
	var seen []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RawQuery)
		if r.URL.Path != "/2.0/repositories/w" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "2" {
			io.WriteString(w, `{"values":[{"n":3}]}`)
			return
		}
		io.WriteString(w, `{"values":[{"n":1},{"n":2}],"next":"https://api.bitbucket.org/2.0/repositories/w?page=2&pagelen=100"}`)
	})
	type item struct{ N int }
	all, err := All[item](context.Background(), c, Path("repositories", "w"), nil)
	if err != nil || len(all) != 3 || all[2].N != 3 {
		t.Fatalf("All = %+v, %v", all, err)
	}
	if strings.Join(seen, " ") != "page=1&pagelen=100 page=2&pagelen=100" {
		t.Errorf("queries = %v", seen)
	}
	if q := ListQuery(gitoperationslib.ListOptions{Page: 3, PerPage: 500}).Encode(); q != "page=3&pagelen=100" {
		t.Errorf("ListQuery = %s (pagelen is capped at 100)", q)
	}
}

func TestPostForm(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %s", r.Header.Get("Content-Type"))
		}
		r.ParseForm()
		if r.Form.Get("README.md") != "# hi" || r.Form.Get("message") != "Initial commit" {
			t.Errorf("form = %v", r.Form)
		}
		w.WriteHeader(http.StatusCreated)
	})
	if _, err := c.PostForm(context.Background(), "repositories/w/r/src", map[string][]string{"README.md": {"# hi"}, "message": {"Initial commit"}}, nil); err != nil {
		t.Fatal(err)
	}
}
