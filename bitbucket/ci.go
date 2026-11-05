package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

var _ gitoperationslib.CI = (*Client)(nil)

// Secrets and Variables manage Bitbucket Pipelines variables, which are one
// list: Secrets are its secured entries, Variables the others. A name used by
// one kind cannot be set as the other. Keys keep their case. Lists return
// everything as one page. User-level variables are not available with API
// tokens: UserCI returns ErrNotSupported. EnvCI reaches the deployment variables
// of an environment, which work the same way.
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

type environmentJSON struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// environment returns the UUID of a repository's environment, waiting briefly
// for a list that lags behind its creation (seen on 2026-11-05).
func (v ci) environment(ctx context.Context, scope gitoperationslib.CIScope) (string, error) {
	if strings.HasPrefix(scope.Environment, "{") {
		return scope.Environment, nil
	}
	for attempt := 1; ; attempt++ {
		all, err := bitbucketapi.All[environmentJSON](ctx, v.c.api, repoPath(scope.Owner, scope.Repo, "environments"), nil)
		if err != nil {
			return "", err
		}
		for _, e := range all {
			if strings.EqualFold(e.Name, scope.Environment) || strings.EqualFold(e.Slug, scope.Environment) {
				return e.UUID, nil
			}
		}
		if attempt == findAttempts {
			return "", &gitoperationslib.APIError{
				Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
				Message: fmt.Sprintf("no environment %q in %s/%s", scope.Environment, scope.Owner, scope.Repo),
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(findDelay):
		}
	}
}

// base is the path of scope's list. For an environment, it looks up its UUID.
func (v ci) base(ctx context.Context, op string, scope gitoperationslib.CIScope) (string, error) {
	switch {
	case scope.Owner == "" && scope.Repo == "" && scope.Environment == "":
		return "", notSupported(op, "user-level Pipelines variables are not available with API tokens")
	case scope.Owner == "":
		return "", fmt.Errorf("bitbucket: %s: a repository scope needs a workspace", op)
	case scope.Environment != "" && scope.Repo == "":
		return "", fmt.Errorf("bitbucket: %s: an environment scope needs a repository", op)
	case scope.Environment != "":
		uuid, err := v.environment(ctx, scope)
		if err != nil {
			return "", err
		}
		return repoPath(scope.Owner, scope.Repo, "deployments_config", "environments", uuid, "variables"), nil
	case scope.Repo != "":
		return repoPath(scope.Owner, scope.Repo, "pipelines_config", "variables"), nil
	}
	return bitbucketapi.Path("workspaces", scope.Owner, "pipelines-config", "variables"), nil
}

// entry is the path of one variable of the list at base.
func entry(base, uuid string) string { return base + "/" + bitbucketapi.Path(uuid) }

// find returns the entry called name, waiting briefly for a list that lags
// behind a write; nil if there is none.
func (v ci) find(ctx context.Context, base, name string) (*pipelineVarJSON, error) {
	for attempt := 1; ; attempt++ {
		all, err := bitbucketapi.All[pipelineVarJSON](ctx, v.c.api, base, nil)
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
	base, err := v.base(ctx, op, scope)
	if err != nil {
		return err
	}
	body := pipelineVarJSON{Key: name, Value: value, Secured: secured}
	_, err = v.c.api.Do(ctx, http.MethodPost, base, nil, body, nil)
	if gitoperationslib.StatusCode(err) != http.StatusConflict {
		return err
	}
	existing, err := v.find(ctx, base, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return notFoundCI(secured, name)
	}
	if existing.Secured != secured {
		return fmt.Errorf("bitbucket: %s: %q is a %s, not a %s: delete it first", op, name, kindOf(existing.Secured), kindOf(secured))
	}
	_, err = v.c.api.Do(ctx, http.MethodPut, entry(base, existing.UUID), nil, body, nil)
	return err
}

// lookup finds the entry called name of the given kind.
func (v ci) lookup(ctx context.Context, base, name string, secured bool) (*pipelineVarJSON, error) {
	existing, err := v.find(ctx, base, name)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.Secured != secured {
		return nil, notFoundCI(secured, name)
	}
	return existing, nil
}

func (v ci) delete(ctx context.Context, op string, scope gitoperationslib.CIScope, name string, secured bool) error {
	base, err := v.base(ctx, op, scope)
	if err != nil {
		return err
	}
	existing, err := v.lookup(ctx, base, name, secured)
	if err != nil {
		return err
	}
	_, err = v.c.api.Do(ctx, http.MethodDelete, entry(base, existing.UUID), nil, nil, nil)
	return err
}

// all lists scope's entries.
func (v ci) all(ctx context.Context, op string, scope gitoperationslib.CIScope) ([]pipelineVarJSON, error) {
	base, err := v.base(ctx, op, scope)
	if err != nil {
		return nil, err
	}
	return bitbucketapi.All[pipelineVarJSON](ctx, v.c.api, base, nil)
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
	base, err := v.base(ctx, "variables.get", scope)
	if err != nil {
		return nil, err
	}
	e, err := v.lookup(ctx, base, name, false)
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
