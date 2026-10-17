package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const releaseResponse = `{
	"id": 11, "tag_name": "v1.0.1", "target_commitish": "main", "name": "1.0.1", "body": "notes",
	"draft": false, "prerelease": false, "author": {"login": "alice"},
	"html_url": "https://h/acme/widget/releases/tag/v1.0.1",
	"tarball_url": "https://h/acme/widget/archive/v1.0.1.tar.gz", "zipball_url": "https://h/acme/widget/archive/v1.0.1.zip",
	"created_at": "2026-10-17T10:00:00Z", "published_at": "2026-10-17T11:00:00Z",
	"assets": [{"id": 21, "name": "tool.tar.gz", "size": 1024, "download_count": 3,
		"browser_download_url": "https://h/attachments/uuid", "type": "attachment", "created_at": "2026-10-17T10:30:00Z"}]
}`

func TestReleaseConversion(t *testing.T) {
	var j releaseJSON
	if err := json.Unmarshal([]byte(releaseResponse), &j); err != nil {
		t.Fatal(err)
	}
	r := j.toRelease()
	if r.ID != 11 || r.TagName != "v1.0.1" || r.TargetCommitish != "main" || r.Name != "1.0.1" || r.Body != "notes" ||
		r.Author != "alice" || r.TarballURL == "" || r.ZipballURL == "" || r.PublishedAt.Hour() != 11 || len(r.Assets) != 1 {
		t.Fatalf("got %+v", r)
	}
	a := r.Assets[0]
	if a.ID != 21 || a.Name != "tool.tar.gz" || a.Size != 1024 || a.DownloadCount != 3 || a.External || a.DownloadURL != "https://h/attachments/uuid" {
		t.Errorf("asset = %+v", a)
	}

	// Drafts report no publication time.
	draft := releaseJSON{Draft: true, PublishedAt: r.PublishedAt}
	if !draft.toRelease().PublishedAt.IsZero() {
		t.Error("draft PublishedAt should be zero")
	}
	if !(&attachmentJSON{Type: "external"}).toAsset().External {
		t.Error("external attachment not flagged")
	}
}

func TestReleasesRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			json.NewDecoder(r.Body).Decode(&body)
		}
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/repos/acme/widget/releases" && r.Method == http.MethodGet:
			io.WriteString(w, "["+releaseResponse+"]")
		case r.URL.Path == "/api/v1/repos/acme/widget/releases/11/assets" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":21,"name":"tool.tar.gz"}]`)
		case strings.Contains(r.URL.Path, "/assets"):
			io.WriteString(w, `{"id":21,"name":"tool.tar.gz","size":3,"type":"attachment"}`)
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

	list, err := rels.List(ctx, "acme", "widget", gitoperationslib.ListOptions{PerPage: 5})
	check(err)
	if len(list.Items) != 1 {
		t.Errorf("List = %+v", list)
	}
	_, err = rels.Get(ctx, "acme", "widget", 11)
	check(err)
	_, err = rels.GetByTag(ctx, "acme", "widget", "v1.0.1")
	check(err)
	_, err = rels.Latest(ctx, "acme", "widget")
	check(err)
	_, err = rels.Create(ctx, "acme", "widget", gitoperationslib.CreateReleaseOptions{
		TagName: "v1.0.1", Name: "1.0.1", Draft: true, HideArchiveLinks: gitoperationslib.Ptr(true),
	})
	check(err)
	_, err = rels.Edit(ctx, "acme", "widget", 11, gitoperationslib.EditReleaseOptions{Draft: gitoperationslib.Ptr(false)})
	check(err)
	check(rels.Delete(ctx, "acme", "widget", 11))
	assets, err := rels.ListAssets(ctx, "acme", "widget", 11, gitoperationslib.ListOptions{})
	check(err)
	if len(assets.Items) != 1 {
		t.Errorf("ListAssets = %+v", assets)
	}
	_, err = rels.GetAsset(ctx, "acme", "widget", 11, 21)
	check(err)
	_, err = rels.EditAsset(ctx, "acme", "widget", 11, 21, gitoperationslib.EditAssetOptions{Name: gitoperationslib.Ptr("new.tar.gz")})
	check(err)
	check(rels.DeleteAsset(ctx, "acme", "widget", 11, 21))

	wantRequests := []string{
		"GET /api/v1/repos/acme/widget/releases?limit=5",
		"GET /api/v1/repos/acme/widget/releases/11?",
		"GET /api/v1/repos/acme/widget/releases/tags/v1.0.1?",
		"GET /api/v1/repos/acme/widget/releases/latest?",
		"POST /api/v1/repos/acme/widget/releases?",
		"PATCH /api/v1/repos/acme/widget/releases/11?",
		"DELETE /api/v1/repos/acme/widget/releases/11?",
		"GET /api/v1/repos/acme/widget/releases/11/assets?",
		"GET /api/v1/repos/acme/widget/releases/11/assets/21?",
		"PATCH /api/v1/repos/acme/widget/releases/11/assets/21?",
		"DELETE /api/v1/repos/acme/widget/releases/11/assets/21?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		4: {"tag_name": "v1.0.1", "name": "1.0.1", "body": "", "draft": true, "prerelease": false, "hide_archive_links": true},
		5: {"draft": false},
		9: {"name": "new.tar.gz"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestUploadAndExternalAsset(t *testing.T) {
	type upload struct{ name, filename, contentType, content, externalURL string }
	var got []upload
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		u := upload{name: r.URL.Query().Get("name"), externalURL: r.FormValue("external_url")}
		if f, h, err := r.FormFile("attachment"); err == nil {
			data, _ := io.ReadAll(f)
			u.filename, u.contentType, u.content = h.Filename, h.Header.Get("Content-Type"), string(data)
		}
		got = append(got, u)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":21,"name":"x","type":"external","browser_download_url":"https://example.com/f"}`)
	})
	ctx := context.Background()
	rels := c.Releases()

	// Content types: guessed from the extension, else octet-stream, else as given.
	if _, err := rels.UploadAsset(ctx, "acme", "widget", 11, "manual.pdf", strings.NewReader("abc"), 3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := rels.UploadAsset(ctx, "acme", "widget", 11, "tool", strings.NewReader("bin"), 3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := rels.UploadAsset(ctx, "acme", "widget", 11, "SUMS", strings.NewReader("xyz"), 3, "text/plain"); err != nil {
		t.Fatal(err)
	}
	ext, ok := rels.(gitoperationslib.ExternalAssets)
	if !ok {
		t.Fatal("Forgejo releases should implement ExternalAssets")
	}
	if _, err := ext.AddExternalAsset(ctx, "acme", "widget", 11, "mirror", "https://example.com/f"); err != nil {
		t.Fatal(err)
	}

	want := []upload{
		{name: "manual.pdf", filename: "manual.pdf", contentType: "application/pdf", content: "abc"},
		{name: "tool", filename: "tool", contentType: "application/octet-stream", content: "bin"},
		{name: "SUMS", filename: "SUMS", contentType: "text/plain", content: "xyz"},
		{name: "mirror", externalURL: "https://example.com/f"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("uploads = %+v\nwant      %+v", got, want)
	}
}

func TestDownloadAsset(t *testing.T) {
	var srvURL string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/widget/releases/11/assets/21":
			fmt.Fprintf(w, `{"id":21,"name":"tool.tar.gz","type":"attachment","browser_download_url":"%s/attachments/uuid"}`, srvURL)
		case "/api/v1/repos/acme/widget/releases/11/assets/22":
			io.WriteString(w, `{"id":22,"name":"mirror","type":"external","browser_download_url":"https://example.com/f"}`)
		case "/attachments/uuid":
			if r.Header.Get("Authorization") != "token pat" {
				t.Error("download without credentials")
			}
			io.WriteString(w, "binary content")
		default:
			http.NotFound(w, r)
		}
	})
	srvURL = strings.TrimSuffix(c.api.BaseURL(), "/api/v1/")

	rc, err := c.Releases().DownloadAsset(context.Background(), "acme", "widget", 11, 21)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "binary content" {
		t.Errorf("got %q", data)
	}

	if _, err := c.Releases().DownloadAsset(context.Background(), "acme", "widget", 11, 22); err == nil {
		t.Error("external asset download should fail")
	}
}

func TestReleasesRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	rels := c.Releases()
	for name, opts := range map[string]gitoperationslib.CreateReleaseOptions{
		"generate notes": {TagName: "v1", GenerateReleaseNotes: true},
		"make latest":    {TagName: "v1", MakeLatest: gitoperationslib.Ptr(true)},
	} {
		if _, err := rels.Create(ctx, "acme", "widget", opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if _, err := rels.Edit(ctx, "acme", "widget", 1, gitoperationslib.EditReleaseOptions{MakeLatest: gitoperationslib.Ptr(false)}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("edit make latest: err = %v", err)
	}
	if _, err := rels.Create(ctx, "acme", "widget", gitoperationslib.CreateReleaseOptions{}); err == nil {
		t.Error("empty tag accepted")
	}
	if _, err := rels.UploadAsset(ctx, "acme", "widget", 1, "", strings.NewReader(""), 0, ""); err == nil {
		t.Error("empty asset name accepted")
	}
}
