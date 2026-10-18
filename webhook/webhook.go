// Package webhook receives webhook deliveries from Forgejo and GitHub: it
// verifies their signature, identifies the forge and event, and decodes the
// common payloads. It does not depend on either backend.
//
//	http.Handle("/hooks", webhook.Handler(secret, func(ctx context.Context, ev *webhook.Event) error {
//		if ev.Name == "push" {
//			push, err := ev.Push()
//			...
//		}
//		return nil
//	}))
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// MaxBodySize is the largest delivery Parse accepts (GitHub caps payloads at
// 25 MB).
const MaxBodySize = 25 << 20

var (
	// ErrMissingSignature is returned for an unsigned delivery.
	ErrMissingSignature = errors.New("webhook: delivery is not signed")
	// ErrInvalidSignature is returned when the signature does not match.
	ErrInvalidSignature = errors.New("webhook: invalid signature")
	// ErrUnknownForge is returned when no known event header is present.
	ErrUnknownForge = errors.New("webhook: not a Forgejo or GitHub delivery")
)

// Verify checks a delivery's HMAC-SHA256 signature. Both forges send
// X-Hub-Signature-256 ("sha256=<hex>"); Forgejo's own X-Forgejo-Signature
// (bare hex) is used when it is absent.
func Verify(body []byte, h http.Header, secret []byte) error {
	if len(secret) == 0 {
		return errors.New("webhook: empty secret")
	}
	var sig string
	if s := h.Get("X-Hub-Signature-256"); s != "" {
		var ok bool
		if sig, ok = strings.CutPrefix(s, "sha256="); !ok {
			return ErrInvalidSignature
		}
	} else if sig = h.Get("X-Forgejo-Signature"); sig == "" {
		return ErrMissingSignature
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrInvalidSignature
	}
	return nil
}

// Event is a decoded delivery.
type Event struct {
	Forge gitoperationslib.Backend
	// Name is the event header value, e.g. "push", "pull_request",
	// "release", or GitHub's "ping".
	Name     string
	Delivery string // unique delivery ID
	Action   string // e.g. "opened", "published"; empty for push
	Repo     string // "owner/name"; empty for events without a repository
	Sender   string // login of the user who triggered the event
	// Payload is the JSON payload, for fields the typed accessors omit.
	Payload []byte
}

// Parse reads, verifies and decodes a delivery. The request body is
// consumed.
func Parse(r *http.Request, secret []byte) (*Event, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, MaxBodySize))
	if err != nil {
		return nil, fmt.Errorf("webhook: read body: %w", err)
	}
	if err := Verify(body, r.Header, secret); err != nil {
		return nil, err
	}
	return Decode(r.Header, body)
}

// Decode identifies and decodes a delivery without verifying it; use Parse
// or call Verify first.
func Decode(h http.Header, body []byte) (*Event, error) {
	ev := &Event{}
	// Forgejo also sends X-GitHub-Event, so it must be checked first.
	switch {
	case h.Get("X-Forgejo-Event") != "":
		ev.Forge, ev.Name, ev.Delivery = gitoperationslib.BackendForgejo, h.Get("X-Forgejo-Event"), h.Get("X-Forgejo-Delivery")
	case h.Get("X-GitHub-Event") != "" && h.Get("X-Gitea-Event") == "":
		ev.Forge, ev.Name, ev.Delivery = gitoperationslib.BackendGitHub, h.Get("X-GitHub-Event"), h.Get("X-GitHub-Delivery")
	default:
		return nil, ErrUnknownForge
	}

	payload, err := jsonPayload(h, body)
	if err != nil {
		return nil, err
	}
	ev.Payload = payload

	var env struct {
		Action     string `json:"action"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Sender *struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("webhook: decode %s payload: %w", ev.Name, err)
	}
	ev.Action = env.Action
	if env.Repository != nil {
		ev.Repo = env.Repository.FullName
	}
	if env.Sender != nil {
		ev.Sender = env.Sender.Login
	}
	return ev, nil
}

// jsonPayload unwraps form-encoded deliveries (payload=<json>).
func jsonPayload(h http.Header, body []byte) ([]byte, error) {
	ct, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	if ct != "application/x-www-form-urlencoded" {
		return body, nil
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("webhook: decode form body: %w", err)
	}
	payload := form.Get("payload")
	if payload == "" {
		return nil, errors.New("webhook: form body has no payload field")
	}
	return []byte(payload), nil
}

// Handler returns an http.Handler that parses each delivery and passes it to
// fn. It answers 405 for non-POST requests, 401 for missing or invalid
// signatures, 400 for undecodable deliveries, 500 when fn fails and 204
// otherwise.
func Handler(secret []byte, fn func(context.Context, *Event) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ev, err := Parse(r, secret)
		switch {
		case errors.Is(err, ErrMissingSignature), errors.Is(err, ErrInvalidSignature):
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := fn(r.Context(), ev); err != nil {
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
