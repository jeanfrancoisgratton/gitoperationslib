package forgejoapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestText(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/plain" {
			t.Errorf("Accept = %q", got)
		}
		if r.URL.Path == "/api/v1/missing" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"not here"}`)
			return
		}
		io.WriteString(w, "1b2e73da")
	})
	ctx := context.Background()
	if got, err := c.Text(ctx, "user/gpg_key_token"); err != nil || got != "1b2e73da" {
		t.Errorf("Text = %q, %v", got, err)
	}
	if _, err := c.Text(ctx, "missing"); !gitoperationslib.IsNotFound(err) || !strings.Contains(err.Error(), "not here") {
		t.Errorf("missing: err = %v, want a 404 with the server's message", err)
	}
}

func TestBaseURL(t *testing.T) {
	c, err := NewClient("https://git.example.com:3000", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.BaseURL(); got != "https://git.example.com:3000/api/v1/" {
		t.Errorf("BaseURL = %q", got)
	}
}
