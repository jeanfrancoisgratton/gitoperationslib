package forgejo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want Version
	}{
		{"15.0.2+gitea-1.22.0", Version{15, 0, 2}},
		{"11.0.10", Version{11, 0, 10}},
		{"v12.1.0", Version{12, 1, 0}},
		{"16.0.0-dev-123-abcdef+gitea-1.22.0", Version{16, 0, 0}},
	}
	for _, tt := range tests {
		got, err := ParseVersion(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "15", "15.0", "1.22.0.1", "a.b.c", "15.-1.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) succeeded", bad)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b Version
		want int
	}{
		{Version{11, 0, 10}, Version{11, 0, 10}, 0},
		{Version{11, 0, 9}, Version{11, 0, 10}, -1},
		{Version{11, 1, 0}, Version{11, 0, 10}, 1},
		{Version{10, 9, 99}, Version{11, 0, 0}, -1},
	}
	for _, tt := range tests {
		if got := tt.a.Compare(tt.b); got != tt.want {
			t.Errorf("%v.Compare(%v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// versionServer answers /api/v1/version with version and records the
// Authorization header it received.
func versionServer(t *testing.T, version string, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/version" {
			http.NotFound(w, r)
			return
		}
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		io.WriteString(w, `{"version":"`+version+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewToken(t *testing.T) {
	var auth string
	srv := versionServer(t, "15.0.2+gitea-1.22.0", &auth)
	c, err := New(context.Background(), srv.URL+"/api/v1/", Token("pat"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "token pat" {
		t.Errorf("Authorization = %q", auth)
	}
	if c.Version() != (Version{15, 0, 2}) {
		t.Errorf("Version() = %v", c.Version())
	}
}

func TestNewBasicAuth(t *testing.T) {
	var auth string
	srv := versionServer(t, "11.0.10", &auth)
	if _, err := New(context.Background(), srv.URL, BasicAuth("jf", "pw"), WithHTTPClient(srv.Client())); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + "amY6cHc=" // base64("jf:pw")
	if auth != want {
		t.Errorf("Authorization = %q, want %q", auth, want)
	}
}

func TestNewAnonymous(t *testing.T) {
	var auth string
	srv := versionServer(t, "15.0.2", &auth)
	if _, err := New(context.Background(), srv.URL, nil, WithHTTPClient(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Errorf("Authorization = %q, want none", auth)
	}
}

func TestNewTooOld(t *testing.T) {
	srv := versionServer(t, "11.0.9+gitea-1.22.0", nil)
	_, err := New(context.Background(), srv.URL, nil, WithHTTPClient(srv.Client()))
	var uv *gitoperationslib.UnsupportedVersionError
	if !errors.As(err, &uv) {
		t.Fatalf("err = %v, want *UnsupportedVersionError", err)
	}
	if uv.Version != "11.0.9+gitea-1.22.0" || uv.Minimum != "11.0.10" {
		t.Errorf("got %+v", uv)
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New(context.Background(), "not a url", nil); err == nil {
		t.Error("invalid URL accepted")
	}

	srv := versionServer(t, "garbage", nil)
	if _, err := New(context.Background(), srv.URL, nil, WithHTTPClient(srv.Client())); err == nil {
		t.Error("unparseable version accepted")
	}

	notFound := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(notFound.Close)
	if _, err := New(context.Background(), notFound.URL, nil, WithHTTPClient(notFound.Client())); !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v, want 404 APIError", err)
	}
}
