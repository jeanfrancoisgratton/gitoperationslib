package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	gh "github.com/google/go-github/v93/github"
	"golang.org/x/crypto/nacl/box"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.CI = (*Client)(nil)

// Secrets and Variables manage GitHub Actions secrets and variables. They
// need the repo scope (classic tokens) or the "Secrets"/"Variables"
// repository permissions (fine-grained tokens); organisation-level ones the
// admin:org scope or the matching organisation permissions. GitHub has no
// user-level Actions secrets (user secrets are Codespaces'): UserCI returns
// ErrNotSupported.
func (c *Client) Secrets() gitoperationslib.Secrets     { return secrets{c} }
func (c *Client) Variables() gitoperationslib.Variables { return variables{c} }

// orgVisibility makes organisation secrets and variables available to all
// its repositories, as on Forgejo.
const orgVisibility = "all"

const noUserCI = "GitHub Actions has no user-level secrets or variables"

// checkCIScope rejects the user scope and incomplete repository scopes.
func checkCIScope(op string, scope gitoperationslib.CIScope) error {
	switch {
	case scope.Owner == "" && scope.Repo == "":
		return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, noUserCI)
	case scope.Owner == "":
		return fmt.Errorf("github: %s: a repository scope needs an owner", op)
	}
	return nil
}

type secrets struct{ c *Client }

func (s secrets) List(ctx context.Context, scope gitoperationslib.CIScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
	if err := checkCIScope("secrets.list", scope); err != nil {
		return gitoperationslib.Page[gitoperationslib.Secret]{}, err
	}
	lo := ghList(opts)
	var list *gh.Secrets
	var resp *gh.Response
	var err error
	if scope.Repo != "" {
		list, resp, err = s.c.gh.Actions.ListRepoSecrets(ctx, scope.Owner, scope.Repo, &lo)
	} else {
		list, resp, err = s.c.gh.Actions.ListOrgSecrets(ctx, scope.Owner, &lo)
	}
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Secret]{}, convertError(err)
	}
	return toPage(list.Secrets, resp, func(s *gh.Secret) gitoperationslib.Secret {
		return gitoperationslib.Secret{Name: s.Name, CreatedAt: s.CreatedAt.Time, UpdatedAt: s.UpdatedAt.Time}
	}), nil
}

// Set encrypts value for the scope's public key (libsodium sealed box), as
// GitHub requires, then stores it.
func (s secrets) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	if err := checkCIScope("secrets.set", scope); err != nil {
		return err
	}
	if name == "" {
		return errors.New("github: secrets.set: name is required")
	}
	var key *gh.PublicKey
	var err error
	if scope.Repo != "" {
		key, _, err = s.c.gh.Actions.GetRepoPublicKey(ctx, scope.Owner, scope.Repo)
	} else {
		key, _, err = s.c.gh.Actions.GetOrgPublicKey(ctx, scope.Owner)
	}
	if err != nil {
		return convertError(err)
	}
	sealed, err := seal(key.GetKey(), value)
	if err != nil {
		return err
	}
	if scope.Repo != "" {
		_, err = s.c.gh.Actions.CreateOrUpdateRepoSecret(ctx, scope.Owner, scope.Repo, name, gh.SecretRequest{KeyID: key.GetKeyID(), EncryptedValue: sealed})
	} else {
		_, err = s.c.gh.Actions.CreateOrUpdateOrgSecret(ctx, scope.Owner, name, gh.SecretOrgRequest{KeyID: key.GetKeyID(), EncryptedValue: sealed, Visibility: orgVisibility})
	}
	return convertError(err)
}

// seal encrypts value for the base64-encoded Curve25519 public key, and
// returns the result base64-encoded.
func seal(publicKey, value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("github: unexpected Actions public key %q", publicKey)
	}
	var recipient [32]byte
	copy(recipient[:], raw)
	sealed, err := box.SealAnonymous(nil, []byte(value), &recipient, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("github: encrypt secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (s secrets) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	if err := checkCIScope("secrets.delete", scope); err != nil {
		return err
	}
	var err error
	if scope.Repo != "" {
		_, err = s.c.gh.Actions.DeleteRepoSecret(ctx, scope.Owner, scope.Repo, name)
	} else {
		_, err = s.c.gh.Actions.DeleteOrgSecret(ctx, scope.Owner, name)
	}
	return convertError(err)
}

type variables struct{ c *Client }

func toVariable(v *gh.ActionsVariable) gitoperationslib.Variable {
	return gitoperationslib.Variable{Name: v.Name, Value: v.Value, CreatedAt: v.GetCreatedAt().Time, UpdatedAt: v.GetUpdatedAt().Time}
}

func (v variables) List(ctx context.Context, scope gitoperationslib.CIScope, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
	if err := checkCIScope("variables.list", scope); err != nil {
		return gitoperationslib.Page[gitoperationslib.Variable]{}, err
	}
	lo := ghList(opts)
	var list *gh.ActionsVariables
	var resp *gh.Response
	var err error
	if scope.Repo != "" {
		list, resp, err = v.c.gh.Actions.ListRepoVariables(ctx, scope.Owner, scope.Repo, &lo)
	} else {
		list, resp, err = v.c.gh.Actions.ListOrgVariables(ctx, scope.Owner, &lo)
	}
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Variable]{}, convertError(err)
	}
	return toPage(list.Variables, resp, toVariable), nil
}

func (v variables) Get(ctx context.Context, scope gitoperationslib.CIScope, name string) (*gitoperationslib.Variable, error) {
	if err := checkCIScope("variables.get", scope); err != nil {
		return nil, err
	}
	var out *gh.ActionsVariable
	var err error
	if scope.Repo != "" {
		out, _, err = v.c.gh.Actions.GetRepoVariable(ctx, scope.Owner, scope.Repo, name)
	} else {
		out, _, err = v.c.gh.Actions.GetOrgVariable(ctx, scope.Owner, name)
	}
	if err != nil {
		return nil, convertError(err)
	}
	vr := toVariable(out)
	return &vr, nil
}

// Set updates the variable, and creates it when the update finds none. An
// existing organisation variable keeps its visibility.
func (v variables) Set(ctx context.Context, scope gitoperationslib.CIScope, name, value string) error {
	if err := checkCIScope("variables.set", scope); err != nil {
		return err
	}
	if name == "" {
		return errors.New("github: variables.set: name is required")
	}
	var err error
	if scope.Repo != "" {
		_, err = v.c.gh.Actions.UpdateRepoVariable(ctx, scope.Owner, scope.Repo, name, gh.ActionsUpdateVariableRequest{Value: &value})
		if err = convertError(err); gitoperationslib.IsNotFound(err) {
			_, err = v.c.gh.Actions.CreateRepoVariable(ctx, scope.Owner, scope.Repo, gh.ActionsCreateVariableRequest{Name: name, Value: value})
			err = convertError(err)
		}
		return err
	}
	_, err = v.c.gh.Actions.UpdateOrgVariable(ctx, scope.Owner, name, gh.ActionsUpdateOrgVariableRequest{Value: &value})
	if err = convertError(err); gitoperationslib.IsNotFound(err) {
		_, err = v.c.gh.Actions.CreateOrgVariable(ctx, scope.Owner, gh.ActionsCreateOrgVariableRequest{Name: name, Value: value, Visibility: orgVisibility})
		err = convertError(err)
	}
	return err
}

func (v variables) Delete(ctx context.Context, scope gitoperationslib.CIScope, name string) error {
	if err := checkCIScope("variables.delete", scope); err != nil {
		return err
	}
	var err error
	if scope.Repo != "" {
		_, err = v.c.gh.Actions.DeleteRepoVariable(ctx, scope.Owner, scope.Repo, name)
	} else {
		_, err = v.c.gh.Actions.DeleteOrgVariable(ctx, scope.Owner, name)
	}
	return convertError(err)
}
