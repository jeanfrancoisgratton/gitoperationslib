package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

var _ gitoperationslib.CI = (*Client)(nil)

// Secrets and Variables manage Bitbucket Pipelines variables, which are one
// list: Secrets are its secured entries, Variables the others. A name used by
// one kind cannot be set as the other. Keys keep their case. Lists return
// everything as one page. User-level variables are not available with API
// tokens: UserCI returns ErrNotSupported.
func (c *Client) Secrets() gitoperationslib.Secrets     { return secrets{ci{c}} }
func (c *Client) Variables() gitoperationslib.Variables { return variables{ci{c}} }

type pipelineVarJSON struct {
	UUID    string `json:"uuid,omitempty"`
	Key     string `json:"key"`
	Value   string `json:"value,omitempty"`
	Secured bool   `json:"secured"`
}

// findAttempts and findDelay cover the workspace list's lag behind writes
// (about half a second, measured 2026-10-30).
const (
	findAttempts = 3
	findDelay    = time.Second
)

type ci struct{ c *Client }

func (v ci) path(op string, scope gitoperationslib.CIScope, uuid string) (string, error) {
	var segs []string
	switch {
	case scope.Owner == "" && scope.Repo == "":
		return "", notSupported(op, "user-level Pipelines variables are not available with API tokens")
	case scope.Owner == "":
		return "", fmt.Errorf("bitbucket: %s: a repository scope needs a workspace", op)
	case scope.Repo != "":
		segs = []string{"repositories", scope.Owner, scope.Repo, "pipelines_config", "variables"}
	default:
		segs = []string{"workspaces", scope.Owner, "pipelines-config", "variables"}
	}
	if uuid != "" {
		segs = append(segs, uuid)
	}
	return bitbucketapi.Path(segs...), nil
}

func (v ci) all(ctx context.Context, op string, scope gitoperationslib.CIScope) ([]pipelineVarJSON, error) {
	path, err := v.path(op, scope, "")
	if err != nil {
		return nil, err
	}
	return bitbucketapi.All[pipelineVarJSON](ctx, v.c.api, path, nil)
}

// find returns the entry called name, waiting briefly for a list that lags
// behind a write; nil if there is none.
func (v ci) find(ctx context.Context, op string, scope gitoperationslib.CIScope, name string) (*pipelineVarJSON, error) {
	for attempt := 1; ; attempt++ {
		all, err := v.all(ctx, op, scope)
		if err != nil {
			return nil, err
		}
		for i := range all {
			if all[i].Key == name {
				return &all[i], nil
			}
		}
		if attempt == findAttempts {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(findDelay):
		}
	}
}

func kindOf(secured bool) string {
	if secured {
		return "secret"
	}
	return "variable"
}

func notFoundCI(secured bool, name string) error {
	return &gitoperationslib.APIError{Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("no %s called %q", kindOf(secured), name)}
}

// set creates the entry, or updates it when the name exists (HTTP 409).
func (v ci) set(ctx context.Context, op string, scope gitoperationslib.CIScope, name, value string, secured bool) error {
	if name == "" {
		return errors.New("bitbucket: " + op + ": name is required")
	}
	path, err := v.path(op, scope, "")
	if err != nil {
		return err
	}
	body := pipelineVarJSON{Key: name, Value: value, Secured: secured}
	_, err = v.c.api.Do(ctx, http.MethodPost, path, nil, body, nil)
	if gitoperationslib.StatusCode(err) != http.StatusConflict {
		return err
	}
	existing, err := v.find(ctx, op, scope, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return notFoundCI(secured, name)
	}
	if existing.Secured != secured {
		return fmt.Errorf("bitbucket: %s: %q is a %s, not a %s: delete it first", op, name, kindOf(existing.Secured), kindOf(secured))
	}
	path, err = v.path(op, scope, existing.UUID)
	if err != nil {
		return err
	}
	_, err = v.c.api.Do(ctx, http.MethodPut, path, nil, body, nil)
	return err
}

// lookup finds the entry called name of the given kind.
func (v ci) lookup(ctx context.Context, op string, scope gitoperationslib.CIScope, name string, secured bool) (*pipelineVarJSON, error) {
	existing, err := v.find(ctx, op, scope, name)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.Secured != secured {
		return nil, notFoundCI(secured, name)
	}
	return existing, nil
}

func (v ci) delete(ctx context.Context, op string, scope gitoperationslib.CIScope, name string, secured bool) error {
	existing, err := v.lookup(ctx, op, scope, name, secured)
	if err != nil {
		return err
	}
	path, err := v.path(op, scope, existing.UUID)
	if err != nil {
		return err
	}
	_, err = v.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

type secrets struct{ ci }

func (s secrets) List(ctx context.Context, scope gitoperationslib.CIScope, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
	all, err := s.all(ctx, "secrets.list", scope)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Secret]{}, err
	}
	var page gitoperationslib.Page[gitoperationslib.Secret]
	for _, v := range all {
		if v.Secured {
			page.Items = append(page.Items, gitoperationslib.Secret{Name: v.Key})
		}
	}
	return page, nil
}

func (s secrets) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	return s.set(ctx, "secrets.set", scope, name, value, true)
}

func (s secrets) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	return s.delete(ctx, "secrets.delete", scope, name, true)
}

type variables struct{ ci }

func (v variables) List(ctx context.Context, scope gitoperationslib.CIScope, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
	all, err := v.all(ctx, "variables.list", scope)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Variable]{}, err
	}
	var page gitoperationslib.Page[gitoperationslib.Variable]
	for _, e := range all {
		if !e.Secured {
			page.Items = append(page.Items, gitoperationslib.Variable{Name: e.Key, Value: e.Value})
		}
	}
	return page, nil
}

func (v variables) Get(ctx context.Context, scope gitoperationslib.CIScope, name string) (*gitoperationslib.Variable, error) {
	e, err := v.lookup(ctx, "variables.get", scope, name, false)
	if err != nil {
		return nil, err
	}
	return &gitoperationslib.Variable{Name: e.Key, Value: e.Value}, nil
}

func (v variables) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	return v.set(ctx, "variables.set", scope, name, value, false)
}

func (v variables) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	return v.delete(ctx, "variables.delete", scope, name, false)
}
