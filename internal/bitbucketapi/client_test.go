package bitbucketapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
	if strings.Join(seen, " ") != "pagelen=100 page=2&pagelen=100" {
		t.Errorf("queries = %v", seen)
	}
	if q := ListQuery(gitoperationslib.ListOptions{Page: 3, PerPage: 500}).Encode(); q != "page=3&pagelen=100" {
		t.Errorf("ListQuery = %s (pagelen is capped at 100)", q)
	}
}

func TestAllFollowsPageTokens(t *testing.T) {
	var pages []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		switch page {
		case "":
			io.WriteString(w, `{"values":[{"n":1}],"next":"https://api.bitbucket.org/2.0/x?pagelen=100&page=67Fg"}`)
		case "67Fg":
			io.WriteString(w, `{"values":[{"n":2}],"next":"https://api.bitbucket.org/2.0/x?pagelen=100&page=8aZ"}`)
		default:
			io.WriteString(w, `{"values":[{"n":3}]}`)
		}
	})
	type item struct{ N int }
	all, err := All[item](context.Background(), c, Path("x"), nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("All = %+v, %v", all, err)
	}
	if strings.Join(pages, " ") != " 67Fg 8aZ" {
		t.Errorf("pages = %q", pages)
	}
}

func TestStream(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/plain" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("headers = %v", r.Header)
		}
		if r.URL.Path == "/2.0/gone" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"type":"error","error":{"message":"nope"}}`)
			return
		}
		io.WriteString(w, "diff")
	})
	stream, err := c.Stream(context.Background(), "ok")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if data, _ := io.ReadAll(stream); string(data) != "diff" {
		t.Errorf("body = %q", data)
	}
	if _, err := c.Stream(context.Background(), "gone"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v, want 404", err)
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

func TestListPaged(t *testing.T) {
	tests := []struct {
		name  string
		pages map[string]string
		want  []int
		next  int
	}{
		{
			name:  "numbered: one page, and the next one's number",
			pages: map[string]string{"": `{"values":[1],"next":"https://x/?page=2&ctx=abc"}`},
			want:  []int{1}, next: 2,
		},
		{
			name:  "last page",
			pages: map[string]string{"": `{"values":[1]}`},
			want:  []int{1},
		},
		{
			name: "next token, as webhooks use: all at once",
			pages: map[string]string{
				"":  `{"values":[1],"next":"https://x/?next=aa-bb&pagelen=1"}`,
				"n": `{"values":[2],"next":"https://x/?next=cc-dd&pagelen=1"}`,
			},
			want: []int{1, 2, 2},
		},
		{
			name: "page token, as a pull request's commits use: all at once",
			pages: map[string]string{
				"":  `{"values":[1],"next":"https://x/?page=67Fg"}`,
				"p": `{"values":[2]}`,
			},
			want: []int{1, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch q := r.URL.Query(); {
				case q.Get("page") == "67Fg":
					io.WriteString(w, tt.pages["p"])
				case q.Get("next") == "cc-dd":
					io.WriteString(w, `{"values":[2]}`)
				case q.Get("next") != "":
					io.WriteString(w, tt.pages["n"])
				default:
					seen["first"]++
					io.WriteString(w, tt.pages[""])
				}
			}))
			defer srv.Close()
			c, err := NewClient(srv.URL+"/2.0", srv.Client(), nil, "ua")
			if err != nil {
				t.Fatal(err)
			}
			got, next, err := ListPaged[int](context.Background(), c, "things", url.Values{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) || next != tt.next || seen["first"] != 1 {
				t.Errorf("got %v, next %d, first page read %d times; want %v, next %d", got, next, seen["first"], tt.want, tt.next)
			}
		})
	}
}
