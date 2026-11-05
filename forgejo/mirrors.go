package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.Mirrors = (*Client)(nil)

type pushMirrorJSON struct {
	RemoteName    string    `json:"remote_name"`
	RemoteAddress string    `json:"remote_address"`
	BranchFilter  string    `json:"branch_filter"`
	Interval      string    `json:"interval"`
	SyncOnCommit  bool      `json:"sync_on_commit"`
	PublicKey     string    `json:"public_key"`
	LastError     string    `json:"last_error"`
	LastUpdate    time.Time `json:"last_update"`
	Created       time.Time `json:"created"`
}

func (j *pushMirrorJSON) toPushMirror() gitoperationslib.PushMirror {
	m := gitoperationslib.PushMirror{
		Name: j.RemoteName, RemoteAddress: j.RemoteAddress, BranchFilter: j.BranchFilter, SyncOnCommit: j.SyncOnCommit,
		PublicKey: j.PublicKey, LastError: j.LastError, CreatedAt: j.Created,
	}
	m.Interval, _ = time.ParseDuration(j.Interval)
	// A mirror that was never pushed reports the Unix epoch.
	if j.LastUpdate.Unix() > 0 {
		m.LastSync = j.LastUpdate
	}
	return m
}

// emptyMessage is what Forgejo sends when its handler has no error to report:
// a Go formatting of nil (checked on 15.0.11).
const emptyMessage = "%!s("

// describeMissing gives a message to an HTTP 404 on a push mirror's name.
func describeMissing(err error, name string) error {
	var apiErr *gitoperationslib.APIError
	if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Message, emptyMessage) {
		apiErr.Message = fmt.Sprintf("push mirror %q not found", name)
	}
	return err
}

// createError converts the error of creating a push mirror: HTTP 400 for an
// interval below the minimum has an empty message, and a remote the server will
// not push to is HTTP 401 "Permission denied", which is about the address and
// not the caller (checked on 15.0.11 with a host that does not resolve).
func createError(err error, address string) error {
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusBadRequest && strings.HasPrefix(apiErr.Message, emptyMessage):
		apiErr.Message = "the interval is below the server's minimum"
	case apiErr.StatusCode == http.StatusBadRequest && apiErr.Message == "Unknown error":
		apiErr.Message = fmt.Sprintf("the server refuses the remote address %q: since Forgejo 17 that includes plain http", address)
	case apiErr.StatusCode == http.StatusUnauthorized && apiErr.Message == "Permission denied":
		apiErr.StatusCode = http.StatusBadRequest
		apiErr.Message = fmt.Sprintf("the server refuses the remote address %q", address)
	}
	return err
}

// ListPushMirrors implements gitoperationslib.Mirrors.
func (c *Client) ListPushMirrors(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.PushMirror], error) {
	return listAt(c, repoPath(owner, repo, "push_mirrors"), (*pushMirrorJSON).toPushMirror)(ctx, opts)
}

// GetPushMirror implements gitoperationslib.Mirrors.
func (c *Client) GetPushMirror(ctx context.Context, owner, repo, name string) (*gitoperationslib.PushMirror, error) {
	if name == "" {
		return nil, errors.New("forgejo: mirrors.get: name is required")
	}
	var out pushMirrorJSON
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "push_mirrors", name), nil, nil, &out); err != nil {
		return nil, describeMissing(err, name)
	}
	m := out.toPushMirror()
	return &m, nil
}

// CreatePushMirror implements gitoperationslib.Mirrors.
func (c *Client) CreatePushMirror(ctx context.Context, owner, repo string, opts gitoperationslib.CreatePushMirrorOptions) (*gitoperationslib.PushMirror, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("forgejo: mirrors.create: %w", err)
	}
	body := struct {
		RemoteAddress  string `json:"remote_address"`
		RemoteUsername string `json:"remote_username,omitempty"`
		RemotePassword string `json:"remote_password,omitempty"`
		UseSSH         bool   `json:"use_ssh,omitempty"`
		Interval       string `json:"interval"`
		SyncOnCommit   bool   `json:"sync_on_commit,omitempty"`
		BranchFilter   string `json:"branch_filter,omitempty"`
	}{opts.RemoteAddress, opts.Username, opts.Password, opts.UseSSH, opts.Interval.String(), opts.SyncOnCommit, opts.BranchFilter}
	var out pushMirrorJSON
	if _, err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "push_mirrors"), nil, body, &out); err != nil {
		return nil, createError(err, opts.RemoteAddress)
	}
	m := out.toPushMirror()
	return &m, nil
}

// DeletePushMirror implements gitoperationslib.Mirrors.
func (c *Client) DeletePushMirror(ctx context.Context, owner, repo, name string) error {
	if name == "" {
		return errors.New("forgejo: mirrors.delete: name is required")
	}
	_, err := c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "push_mirrors", name), nil, nil, nil)
	return describeMissing(err, name)
}

// SyncPushMirrors implements gitoperationslib.Mirrors.
func (c *Client) SyncPushMirrors(ctx context.Context, owner, repo string) error {
	_, err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "push_mirrors-sync"), nil, nil, nil)
	return err
}

// SyncMirror implements gitoperationslib.Mirrors.
func (c *Client) SyncMirror(ctx context.Context, owner, repo string) error {
	_, err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "mirror-sync"), nil, nil, nil)
	return err
}
