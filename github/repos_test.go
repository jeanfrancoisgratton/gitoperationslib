package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fakeGitHub serves /user (login "me") and /orgs/{name} (only "acme" exists)
// under /api/v3; every other request goes to handler.
func fakeGitHub(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"login":"me"}`)
	})
	mux.HandleFunc("GET /api/v3/orgs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "acme" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`)
			return
		}
		io.WriteString(w, `{"login":"acme"}`)
	})
	mux.HandleFunc("/", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New("pat", WithEnterpriseURL(srv.URL, ""), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const repoResponse = `{
	"id": 7, "owner": {"login": "acme"}, "name": "widget", "full_name": "acme/widget",
	"description": "d", "homepage": "https://w", "private": true, "visibility": "private",
	"default_branch": "main", "fork": false, "archived": false, "is_template": true,
	"html_url": "https://github.com/acme/widget", "clone_url": "https://github.com/acme/widget.git",
	"ssh_url": "git@github.com:acme/widget.git",
	"created_at": "2026-10-17T10:00:00Z", "updated_at": "2026-10-17T11:00:00Z"
}`

func TestRepoConversion(t *testing.T) {
	var j gh.Repository
	if err := json.Unmarshal([]byte(repoResponse), &j); err != nil {
		t.Fatal(err)
	}
	r := toRepo(&j)
	if r.ID != 7 || r.Owner != "acme" || r.Name != "widget" || r.FullName != "acme/widget" ||
		r.Description != "d" || r.Website != "https://w" || r.Visibility != gitoperationslib.VisibilityPrivate ||
		r.DefaultBranch != "main" || !r.Template || r.Mirror || r.CloneURL != "https://github.com/acme/widget.git" ||
		r.SSHURL != "git@github.com:acme/widget.git" || r.CreatedAt.Hour() != 10 || r.UpdatedAt.Hour() != 11 {
		t.Errorf("got %+v", r)
	}

	for _, tt := range []struct {
		visibility string
		private    bool
		want       gitoperationslib.Visibility
	}{
		{"internal", true, gitoperationslib.VisibilityInternal},
		{"public", false, gitoperationslib.VisibilityPublic},
		{"", true, gitoperationslib.VisibilityPrivate}, // older GHES without visibility
		{"", false, gitoperationslib.VisibilityPublic},
	} {
		repo := &gh.Repository{Private: gh.Ptr(tt.private)}
		if tt.visibility != "" {
			repo.Visibility = gh.Ptr(tt.visibility)
		}
		if got := toRepo(repo).Visibility; got != tt.want {
			t.Errorf("visibility=%q private=%v: %q, want %q", tt.visibility, tt.private, got, tt.want)
		}
	}
}

func TestCreateRepoEndpoint(t *testing.T) {
	for owner, want := range map[string]string{
		"":     "/api/v3/user/repos",
		"me":   "/api/v3/user/repos",
		"ME":   "/api/v3/user/repos",
		"acme": "/api/v3/orgs/acme/repos",
	} {
		var got string
		c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.Method + " " + r.URL.Path
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, repoResponse)
		})
		if _, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{Owner: owner, Name: "widget"}); err != nil {
			t.Fatalf("owner %q: %v", owner, err)
		}
		if got != "POST "+want {
			t.Errorf("owner %q: request = %q, want POST %s", owner, got, want)
		}
	}
}

func TestCreateRepoBody(t *testing.T) {
	var got map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, repoResponse)
	})
	_, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{
		Name: "widget", Description: "d", Visibility: gitoperationslib.VisibilityInternal,
		AutoInit: true, Gitignores: []string{"Go"}, License: "mit",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "widget", "description": "d", "visibility": "internal", "auto_init": true,
		"is_template": false, "gitignore_template": "Go", "license_template": "mit",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("body = %v\nwant   %v", got, want)
	}

	// Public/private must use the private flag: user-owned creates ignore
	// the visibility field.
	for vis, want := range map[gitoperationslib.Visibility]bool{
		gitoperationslib.VisibilityPrivate: true,
		gitoperationslib.VisibilityPublic:  false,
	} {
		if _, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{Name: "widget", Visibility: vis}); err != nil {
			t.Fatal(err)
		}
		if _, hasVis := got["visibility"]; got["private"] != want || hasVis {
			t.Errorf("%s: body = %v", vis, got)
		}
	}
}

func TestCreateRepoRejected(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	for name, opts := range map[string]gitoperationslib.CreateRepoOptions{
		"default branch": {Name: "x", DefaultBranch: "main"},
		"two gitignores": {Name: "x", Gitignores: []string{"Go", "JetBrains"}},
		"other user":     {Name: "x", Owner: "someone"},
	} {
		if _, err := c.Repos().Create(ctx, opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", Visibility: "secret"}); err == nil {
		t.Error("invalid visibility accepted")
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{}); err == nil {
		t.Error("empty name accepted")
	}
}

func TestGetEditDeleteRepo(t *testing.T) {
	var requests []string
	var editBody map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		switch r.Method {
		case http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&editBody)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		}
		io.WriteString(w, repoResponse)
	})
	ctx := context.Background()

	if repo, err := c.Repos().Get(ctx, "acme", "widget"); err != nil || repo.Name != "widget" {
		t.Fatalf("Get = %+v, %v", repo, err)
	}
	vis := gitoperationslib.VisibilityPublic
	_, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{
		Description: gitoperationslib.Ptr(""), Website: gitoperationslib.Ptr("https://w"), Visibility: &vis, Template: gitoperationslib.Ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(editBody) != fmt.Sprint(map[string]any{"description": "", "homepage": "https://w", "private": false, "is_template": false}) {
		t.Errorf("edit body = %v", editBody)
	}
	if err := c.Repos().Delete(ctx, "acme", "widget"); err != nil {
		t.Fatal(err)
	}
	want := "[GET /api/v3/repos/acme/widget PATCH /api/v3/repos/acme/widget DELETE /api/v3/repos/acme/widget]"
	if fmt.Sprint(requests) != want {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}

	for _, bad := range []gitoperationslib.Visibility{"secret", ""} {
		if _, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{Visibility: &bad}); err == nil {
			t.Errorf("invalid visibility %q accepted", bad)
		}
	}
}

func TestListReposEndpoint(t *testing.T) {
	for owner, want := range map[string]string{
		"":        "/api/v3/user/repos?",
		"me":      "/api/v3/user/repos?affiliation=owner",
		"acme":    "/api/v3/orgs/acme/repos?",
		"someone": "/api/v3/users/someone/repos?",
	} {
		var got string
		c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path + "?" + r.URL.RawQuery
			io.WriteString(w, "[]")
		})
		if _, err := c.Repos().List(context.Background(), owner, gitoperationslib.ListOptions{}); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("owner %q: request = %s, want %s", owner, got, want)
		}
	}
}

func TestListReposPaging(t *testing.T) {
	const total = 5
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page = max(page, 1)
		var items []string
		for id := (page-1)*perPage + 1; id <= min(page*perPage, total); id++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"r%d"}`, id, id))
		}
		if page*perPage < total {
			w.Header().Set("Link", fmt.Sprintf(`<http://h%s?per_page=%d&page=%d>; rel="next"`, r.URL.Path, perPage, page+1))
		}
		fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
	})

	var ids []int64
	fetch := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return c.Repos().List(ctx, "acme", o)
	}
	for repo, err := range gitoperationslib.All(context.Background(), gitoperationslib.ListOptions{PerPage: 2}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, repo.ID)
	}
	if fmt.Sprint(ids) != "[1 2 3 4 5]" {
		t.Errorf("ids = %v", ids)
	}
}

func TestErrorConversion(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		header     map[string]string
		body       string
		wantStatus int
		wantString string
	}{
		{
			name: "validation", status: 422,
			body:       `{"message":"Repository creation failed.","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"},{"resource":"Repository","field":"name","code":"missing_field"}],"documentation_url":"https://docs.github.com/rest/repos"}`,
			wantStatus: 422,
			wantString: "github: HTTP 422: Repository creation failed. [name already exists on this account; Repository.name: missing_field]",
		},
		{
			name: "not found", status: 404,
			body:       `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`,
			wantStatus: 404,
			wantString: "github: HTTP 404: Not Found",
		},
		{
			name: "rate limit", status: 403,
			header:     map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "60", "X-RateLimit-Reset": "1792224000"},
			body:       `{"message":"API rate limit exceeded"}`,
			wantStatus: 403,
			wantString: "github: HTTP 403: API rate limit exceeded [rate limit resets at 2026-10-17T08:00:00Z]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			_, err := c.Repos().Get(context.Background(), "acme", "widget")
			var apiErr *gitoperationslib.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.wantStatus {
				t.Fatalf("err = %#v", err)
			}
			if err.Error() != tt.wantString {
				t.Errorf("Error() = %q\nwant      %q", err.Error(), tt.wantString)
			}
		})
	}
}

func TestContextErrorPassesThrough(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request should not be sent") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Repos().Get(ctx, "acme", "widget"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestClientInterface(t *testing.T) {
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend() != gitoperationslib.BackendGitHub || !c.Capabilities().Has(gitoperationslib.CapRepos) {
		t.Errorf("Backend=%v Capabilities=%v", c.Backend(), c.Capabilities())
	}
}
