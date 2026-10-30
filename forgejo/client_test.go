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

// headerServer answers the version check, and /api/v1/user with a 401 like
// Forgejo's when the OTP header is missing; it records each request's
// headers.
func headerServer(t *testing.T, got *[]http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = append(*got, r.Header.Clone())
		if _, _, ok := r.BasicAuth(); ok && r.Header.Get("X-FORGEJO-OTP") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"invalid provided OTP","url":"https://git.example/api/swagger"}`)
			return
		}
		io.WriteString(w, `{"version":"15.0.11+gitea-1.22.0","login":"me"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewBasicAuthOTP(t *testing.T) {
	var got []http.Header
	srv := headerServer(t, &got)
	ctx := context.Background()

	// Without a code, the 2FA account is refused from the version check on.
	_, err := New(ctx, srv.URL, BasicAuth("jf", "pw"), WithHTTPClient(srv.Client()))
	if !errors.Is(err, gitoperationslib.ErrInvalidOTP) || !gitoperationslib.IsUnauthorized(err) {
		t.Fatalf("BasicAuth on a 2FA account: err = %v, want ErrInvalidOTP (401)", err)
	}

	got = nil
	c, err := New(ctx, srv.URL, BasicAuthOTP("jf", "pw", "123456"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.currentLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d requests, want 2", len(got))
	}
	for i, h := range got {
		if h.Get("Authorization") != "Basic amY6cHc=" || h.Get("X-FORGEJO-OTP") != "123456" {
			t.Errorf("request %d: Authorization %q, OTP %q", i+1, h.Get("Authorization"), h.Get("X-FORGEJO-OTP"))
		}
	}
}

func TestNewBasicAuthOTPFunc(t *testing.T) {
	var got []http.Header
	srv := headerServer(t, &got)
	ctx := context.Background()
	codes := []string{"111111", "222222"}
	calls := 0
	next := func() string { c := codes[min(calls, len(codes)-1)]; calls++; return c }

	c, err := New(ctx, srv.URL, BasicAuthOTPFunc("jf", "pw", next), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.currentLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Get("X-FORGEJO-OTP") != "111111" || got[1].Get("X-FORGEJO-OTP") != "222222" {
		t.Errorf("codes sent: %v, want one fresh code per request", got)
	}
}

func TestWithSudo(t *testing.T) {
	var got []http.Header
	srv := headerServer(t, &got)
	if _, err := New(context.Background(), srv.URL, Token("admin"), WithSudo("alice"), WithHTTPClient(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if h := got[0]; h.Get("Sudo") != "alice" || h.Get("Authorization") != "token admin" {
		t.Errorf("Sudo %q, Authorization %q", h.Get("Sudo"), h.Get("Authorization"))
	}

	got = nil
	if _, err := New(context.Background(), srv.URL, Token("admin"), WithHTTPClient(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if s, ok := got[0]["Sudo"]; ok {
		t.Errorf("Sudo sent without WithSudo: %q", s)
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
