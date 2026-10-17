package forgejoapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestUpload(t *testing.T) {
	content := make([]byte, 300<<10)
	rand.Read(content)

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/x/assets" || r.URL.Query().Get("name") != `a "b".bin` {
			t.Errorf("got %s %s", r.Method, r.URL)
		}
		if r.ContentLength <= int64(len(content)) || len(r.TransferEncoding) != 0 {
			t.Errorf("ContentLength=%d TransferEncoding=%v; want exact length, not chunked", r.ContentLength, r.TransferEncoding)
		}
		if r.Header.Get("Authorization") != "token secret" {
			t.Error("missing auth")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if got := r.FormValue("note"); got != "hello" {
			t.Errorf("note = %q", got)
		}
		f, h, err := r.FormFile("attachment")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(f)
		if !bytes.Equal(got, content) || h.Filename != `a "b".bin` || h.Header.Get("Content-Type") != "application/x-test" {
			t.Errorf("file: %d bytes, filename %q, type %q", len(got), h.Filename, h.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":1}`)
	})

	var out struct{ ID int }
	// Content has trailing bytes beyond Size: only Size bytes may be sent.
	file := &FormFile{Field: "attachment", Filename: `a "b".bin`, ContentType: "application/x-test",
		Content: io.MultiReader(bytes.NewReader(content), strings.NewReader("extra")), Size: int64(len(content))}
	if _, err := c.Upload(context.Background(), "x/assets", url.Values{"name": {`a "b".bin`}}, map[string]string{"note": "hello"}, file, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != 1 {
		t.Errorf("out = %+v", out)
	}
}

func TestUploadFieldsOnly(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("external_url") != "https://example.com/f" || len(r.MultipartForm.File) != 0 {
			t.Errorf("form = %+v", r.MultipartForm)
		}
		io.WriteString(w, `{}`)
	})
	if _, err := c.Upload(context.Background(), "x", nil, map[string]string{"external_url": "https://example.com/f"}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUploadErrors(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(w, `{"message":"file too large"}`)
	})
	file := &FormFile{Field: "attachment", Filename: "f", Content: strings.NewReader("abc"), Size: 3}
	if _, err := c.Upload(context.Background(), "x", nil, nil, file, nil); err == nil || !strings.Contains(err.Error(), "HTTP 413: file too large") {
		t.Errorf("err = %v", err)
	}
	file.Size = -1
	if _, err := c.Upload(context.Background(), "x", nil, nil, file, nil); err == nil {
		t.Error("unknown size accepted")
	}
}

func TestDownload(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token secret" {
			t.Error("missing auth")
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, "payload")
	})
	srvURL := c.baseURL.Scheme + "://" + c.baseURL.Host

	rc, err := c.Download(context.Background(), srvURL+"/attachments/uuid")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "payload" {
		t.Errorf("got %q", got)
	}

	if _, err := c.Download(context.Background(), srvURL+"/missing"); gitoperationslib.StatusCode(err) != 404 {
		t.Errorf("missing: err = %v", err)
	}

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("credentials sent to another host")
	}))
	defer other.Close()
	if _, err := c.Download(context.Background(), other.URL+"/f"); err == nil {
		t.Error("download from another host accepted")
	}
}
