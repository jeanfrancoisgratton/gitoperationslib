package forgejo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var _ gitoperationslib.CI = (*Client)(nil)

// Secrets and Variables manage Forgejo Actions secrets and variables.
func (c *Client) Secrets() gitoperationslib.Secrets     { return secrets{c} }
func (c *Client) Variables() gitoperationslib.Variables { return variables{c} }

// ciPath is the Actions path of scope: repos/{owner}/{repo}/actions/kind,
// orgs/{org}/actions/kind or user/actions/kind, followed by name if any.
func ciPath(scope gitoperationslib.CIScope, kind, name string) (string, error) {
	var segs []string
	switch {
	case scope.Repo != "" && scope.Owner == "":
		return "", errors.New("forgejo: a repository scope needs an owner")
	case scope.Repo != "":
		segs = []string{"repos", scope.Owner, scope.Repo}
	case scope.Owner != "":
		segs = []string{"orgs", scope.Owner}
	default:
		segs = []string{"user"}
	}
	segs = append(segs, "actions", kind)
	if name != "" {
		segs = append(segs, name)
	}
	return forgejoapi.Path(segs...), nil
}

func requireName(op, name string) error {
	if name == "" {
		return errors.New("forgejo: " + op + ": name is required")
	}
	return nil
}

type secrets struct{ c *Client }

type secretJSON struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *secretJSON) toSecret() gitoperationslib.Secret {
	return gitoperationslib.Secret{Name: s.Name, CreatedAt: s.CreatedAt}
}

func (s secrets) List(ctx context.Context, scope gitoperationslib.CIScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
	if scope == gitoperationslib.UserCI() {
		return gitoperationslib.Page[gitoperationslib.Secret]{}, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "secrets.list", "Forgejo cannot list a user's own secrets")
	}
	path, err := ciPath(scope, "secrets", "")
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Secret]{}, err
	}
	return listAt(s.c, path, (*secretJSON).toSecret)(ctx, opts)
}

func (s secrets) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	if err := requireName("secrets.set", name); err != nil {
		return err
	}
	path, err := ciPath(scope, "secrets", name)
	if err != nil {
		return err
	}
	_, err = s.c.api.Do(ctx, http.MethodPut, path, nil, map[string]string{"data": value}, nil)
	return err
}

func (s secrets) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	if err := requireName("secrets.delete", name); err != nil {
		return err
	}
	path, err := ciPath(scope, "secrets", name)
	if err != nil {
		return err
	}
	_, err = s.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

type variables struct{ c *Client }

type variableJSON struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func (v *variableJSON) toVariable() gitoperationslib.Variable {
	return gitoperationslib.Variable{Name: v.Name, Value: v.Data}
}

func (v variables) List(ctx context.Context, scope gitoperationslib.CIScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
	path, err := ciPath(scope, "variables", "")
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Variable]{}, err
	}
	return listAt(v.c, path, (*variableJSON).toVariable)(ctx, opts)
}

func (v variables) Get(ctx context.Context, scope gitoperationslib.CIScope, name string) (*gitoperationslib.Variable, error) {
	if err := requireName("variables.get", name); err != nil {
		return nil, err
	}
	path, err := ciPath(scope, "variables", name)
	if err != nil {
		return nil, err
	}
	var out variableJSON
	if _, err := v.c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	vr := out.toVariable()
	return &vr, nil
}

// Set updates the variable, and creates it when the update finds none.
func (v variables) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	if err := requireName("variables.set", name); err != nil {
		return err
	}
	path, err := ciPath(scope, "variables", name)
	if err != nil {
		return err
	}
	body := map[string]string{"value": value}
	_, err = v.c.api.Do(ctx, http.MethodPut, path, nil, body, nil)
	if gitoperationslib.IsNotFound(err) {
		_, err = v.c.api.Do(ctx, http.MethodPost, path, nil, body, nil)
	}
	return err
}

func (v variables) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	if err := requireName("variables.delete", name); err != nil {
		return err
	}
	path, err := ciPath(scope, "variables", name)
	if err != nil {
		return err
	}
	_, err = v.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}
