package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const releaseResponse = `{
	"id": 11, "tag_name": "v1.0.1", "target_commitish": "main", "name": "1.0.1", "body": "notes",
	"draft": false, "prerelease": true, "author": {"login": "alice"},
	"html_url": "https://github.com/acme/widget/releases/tag/v1.0.1",
	"tarball_url": "https://api.github.com/repos/acme/widget/tarball/v1.0.1",
	"zipball_url": "https://api.github.com/repos/acme/widget/zipball/v1.0.1",
	"created_at": "2026-10-17T10:00:00Z", "published_at": "2026-10-17T11:00:00Z",
	"assets": [{"id": 21, "name": "tool.tar.gz", "size": 1024, "content_type": "application/gzip", "download_count": 3,
		"browser_download_url": "https://github.com/acme/widget/releases/download/v1.0.1/tool.tar.gz"}]
}`

func TestReleaseConversion(t *testing.T) {
	var j gh.RepositoryRelease
	if err := json.Unmarshal([]byte(releaseResponse), &j); err != nil {
		t.Fatal(err)
	}
	r := toRelease(&j)
	if r.ID != 11 || r.TagName != "v1.0.1" || r.Name != "1.0.1" || r.Body != "notes" || !r.Prerelease ||
		r.Author != "alice" || r.TarballURL == "" || r.PublishedAt.Hour() != 11 || len(r.Assets) != 1 {
		t.Fatalf("got %+v", r)
	}
	if a := r.Assets[0]; a.ID != 21 || a.Size != 1024 || a.ContentType != "application/gzip" || a.DownloadCount != 3 || a.External {
		t.Errorf("asset = %+v", a)
	}
	if !toRelease(&gh.RepositoryRelease{Draft: true}).PublishedAt.IsZero() {
		t.Error("draft PublishedAt should be zero")
	}
}

func TestReleasesRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/releases") && r.Method == http.MethodGet:
			io.WriteString(w, "["+releaseResponse+"]")
		case strings.HasSuffix(r.URL.Path, "/assets"):
			io.WriteString(w, `[{"id":21,"name":"tool.tar.gz"}]`)
		case strings.Contains(r.URL.Path, "/assets/"):
			io.WriteString(w, `{"id":21,"name":"tool.tar.gz"}`)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, releaseResponse)
		default:
			io.WriteString(w, releaseResponse)
		}
	})
	ctx := context.Background()
	rels := c.Releases()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := rels.List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	check(err)
	_, err = rels.Get(ctx, "acme", "widget", 11)
	check(err)
	_, err = rels.GetByTag(ctx, "acme", "widget", "v1.0.1")
	check(err)
	_, err = rels.Latest(ctx, "acme", "widget")
	check(err)
	_, err = rels.Create(ctx, "acme", "widget", gitoperationslib.CreateReleaseOptions{
		TagName: "v1.0.1", Draft: true, GenerateReleaseNotes: true, MakeLatest: gitoperationslib.Ptr(false),
	})
	check(err)
	_, err = rels.Edit(ctx, "acme", "widget", 11, gitoperationslib.EditReleaseOptions{Draft: gitoperationslib.Ptr(false), MakeLatest: gitoperationslib.Ptr(true)})
	check(err)
	check(rels.Delete(ctx, "acme", "widget", 11))
	_, err = rels.ListAssets(ctx, "acme", "widget", 11, gitoperationslib.ListOptions{})
	check(err)
	_, err = rels.GetAsset(ctx, "acme", "widget", 11, 21)
	check(err)
	_, err = rels.EditAsset(ctx, "acme", "widget", 11, 21, gitoperationslib.EditAssetOptions{Name: gitoperationslib.Ptr("new.tar.gz")})
	check(err)
	check(rels.DeleteAsset(ctx, "acme", "widget", 11, 21))

	wantRequests := []string{
		"GET /api/v3/repos/acme/widget/releases",
		"GET /api/v3/repos/acme/widget/releases/11",
		"GET /api/v3/repos/acme/widget/releases/tags/v1.0.1",
		"GET /api/v3/repos/acme/widget/releases/latest",
		"POST /api/v3/repos/acme/widget/releases",
		"PATCH /api/v3/repos/acme/widget/releases/11",
		"DELETE /api/v3/repos/acme/widget/releases/11",
		"GET /api/v3/repos/acme/widget/releases/11/assets",
		"GET /api/v3/repos/acme/widget/releases/assets/21",
		"PATCH /api/v3/repos/acme/widget/releases/assets/21",
		"DELETE /api/v3/repos/acme/widget/releases/assets/21",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		4: {"tag_name": "v1.0.1", "draft": true, "prerelease": false, "generate_release_notes": true, "make_latest": "false"},
		5: {"draft": false, "make_latest": "true"},
		9: {"name": "new.tar.gz"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestUploadAsset(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/uploads/repos/acme/widget/releases/11/assets" || r.URL.Query().Get("name") != "tool 1.0.tar.gz" {
			t.Errorf("got %s %s", r.Method, r.URL)
		}
		if r.ContentLength != 5 || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Authorization") != "Bearer pat" {
			t.Errorf("ContentLength=%d Content-Type=%q Authorization=%q", r.ContentLength, r.Header.Get("Content-Type"), r.Header.Get("Authorization"))
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "12345" {
			t.Errorf("body = %q", data)
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":21,"name":"tool 1.0.tar.gz","size":5}`)
	})
	// The reader holds more than size bytes: only size may be sent.
	a, err := c.Releases().UploadAsset(context.Background(), "acme", "widget", 11, "tool 1.0.tar.gz", strings.NewReader("12345extra"), 5, "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 21 || a.Size != 5 {
		t.Errorf("asset = %+v", a)
	}
}

func TestDownloadAssetFollowsRedirectWithoutToken(t *testing.T) {
	var storage string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/acme/widget/releases/assets/21":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("Accept = %q", r.Header.Get("Accept"))
			}
			http.Redirect(w, r, storage+"/storage/signed", http.StatusFound)
		case "/storage/signed":
			if r.Header.Get("Authorization") != "" {
				t.Error("token sent to the storage host")
			}
			io.WriteString(w, "binary content")
		default:
			http.NotFound(w, r)
		}
	})
	storage = strings.TrimSuffix(c.gh.BaseURL(), "/api/v3/")

	rc, err := c.Releases().DownloadAsset(context.Background(), "acme", "widget", 11, 21)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "binary content" {
		t.Errorf("got %q", data)
	}
}

func TestReleasesRejected(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	rels := c.Releases()
	if _, err := rels.Create(ctx, "acme", "widget", gitoperationslib.CreateReleaseOptions{TagName: "v1", HideArchiveLinks: gitoperationslib.Ptr(true)}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("create hide archive links: err = %v", err)
	}
	if _, err := rels.Edit(ctx, "acme", "widget", 1, gitoperationslib.EditReleaseOptions{HideArchiveLinks: gitoperationslib.Ptr(true)}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("edit hide archive links: err = %v", err)
	}
	if _, ok := rels.(gitoperationslib.ExternalAssets); ok {
		t.Error("GitHub releases should not implement ExternalAssets")
	}
	if _, err := rels.Create(ctx, "acme", "widget", gitoperationslib.CreateReleaseOptions{}); err == nil {
		t.Error("empty tag accepted")
	}
	if _, err := rels.UploadAsset(ctx, "acme", "widget", 1, "x", strings.NewReader(""), -1, ""); err == nil {
		t.Error("unknown size accepted")
	}
}
