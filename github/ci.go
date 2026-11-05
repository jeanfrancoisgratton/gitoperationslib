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

var (
	_ gitoperationslib.CI         = (*Client)(nil)
	_ gitoperationslib.OrgSecrets = (*Client)(nil)
)

// Secrets and Variables manage GitHub Actions secrets and variables. They
// need the repo scope (classic tokens) or the "Secrets"/"Variables"
// repository permissions (fine-grained tokens); organisation-level ones the
// admin:org scope or the matching organisation permissions. GitHub has no
// user-level Actions secrets (user secrets are Codespaces'): UserCI returns
// ErrNotSupported.
func (c *Client) Secrets() gitoperationslib.Secrets     { return secrets{c} }
func (c *Client) Variables() gitoperationslib.Variables { return variables{c} }

// orgVisibility makes new organisation secrets and variables available to all
// its repositories, as on Forgejo.
const orgVisibility = string(gitoperationslib.SecretVisibleAll)

const noUserCI = "GitHub Actions has no user-level secrets or variables"

// checkCIScope rejects the user scope and incomplete repository or environment
// scopes.
func checkCIScope(op string, scope gitoperationslib.CIScope) error {
	switch {
	case scope.Owner == "" && scope.Repo == "" && scope.Environment == "":
		return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, noUserCI)
	case scope.Owner == "":
		return fmt.Errorf("github: %s: a repository scope needs an owner", op)
	case scope.Environment != "" && scope.Repo == "":
		return fmt.Errorf("github: %s: an environment scope needs a repository", op)
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
	switch {
	case scope.Environment != "":
		list, resp, err = s.c.gh.Actions.ListEnvSecrets(ctx, scope.Owner, scope.Repo, scope.Environment, &lo)
	case scope.Repo != "":
		list, resp, err = s.c.gh.Actions.ListRepoSecrets(ctx, scope.Owner, scope.Repo, &lo)
	default:
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
	if scope.Repo == "" {
		access, err := s.c.keptOrgAccess(ctx, scope.Owner, name)
		if err != nil {
			return err
		}
		return s.c.setOrgSecret(ctx, scope.Owner, name, value, access)
	}
	var key *gh.PublicKey
	var err error
	if scope.Environment != "" {
		key, _, err = s.c.gh.Actions.GetEnvPublicKey(ctx, scope.Owner, scope.Repo, scope.Environment)
	} else {
		key, _, err = s.c.gh.Actions.GetRepoPublicKey(ctx, scope.Owner, scope.Repo)
	}
	if err != nil {
		return convertError(err)
	}
	sealed, err := seal(key.GetKey(), value)
	if err != nil {
		return err
	}
	request := gh.SecretRequest{KeyID: key.GetKeyID(), EncryptedValue: sealed}
	if scope.Environment != "" {
		_, err = s.c.gh.Actions.CreateOrUpdateEnvSecret(ctx, scope.Owner, scope.Repo, scope.Environment, name, request)
	} else {
		_, err = s.c.gh.Actions.CreateOrUpdateRepoSecret(ctx, scope.Owner, scope.Repo, name, request)
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
	switch {
	case scope.Environment != "":
		_, err = s.c.gh.Actions.DeleteEnvSecret(ctx, scope.Owner, scope.Repo, scope.Environment, name)
	case scope.Repo != "":
		_, err = s.c.gh.Actions.DeleteRepoSecret(ctx, scope.Owner, scope.Repo, name)
	default:
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
	switch {
	case scope.Environment != "":
		list, resp, err = v.c.gh.Actions.ListEnvVariables(ctx, scope.Owner, scope.Repo, scope.Environment, &lo)
	case scope.Repo != "":
		list, resp, err = v.c.gh.Actions.ListRepoVariables(ctx, scope.Owner, scope.Repo, &lo)
	default:
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
	switch {
	case scope.Environment != "":
		out, _, err = v.c.gh.Actions.GetEnvVariable(ctx, scope.Owner, scope.Repo, scope.Environment, name)
	case scope.Repo != "":
		out, _, err = v.c.gh.Actions.GetRepoVariable(ctx, scope.Owner, scope.Repo, name)
	default:
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
	if scope.Environment != "" {
		_, err = v.c.gh.Actions.UpdateEnvVariable(ctx, scope.Owner, scope.Repo, scope.Environment, name, gh.ActionsUpdateVariableRequest{Value: &value})
		if err = convertError(err); gitoperationslib.IsNotFound(err) {
			_, err = v.c.gh.Actions.CreateEnvVariable(ctx, scope.Owner, scope.Repo, scope.Environment, gh.ActionsCreateVariableRequest{Name: name, Value: value})
			err = convertError(err)
		}
		return err
	}
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
	switch {
	case scope.Environment != "":
		_, err = v.c.gh.Actions.DeleteEnvVariable(ctx, scope.Owner, scope.Repo, scope.Environment, name)
	case scope.Repo != "":
		_, err = v.c.gh.Actions.DeleteRepoVariable(ctx, scope.Owner, scope.Repo, name)
	default:
		_, err = v.c.gh.Actions.DeleteOrgVariable(ctx, scope.Owner, name)
	}
	return convertError(err)
}

// orgAccess is an organisation secret's visibility, with the repositories' IDs
// when they are selected.
type orgAccess struct {
	visibility string
	repoIDs    []int64
}

// setOrgSecret stores an organisation secret. The request always holds its
// visibility, which GitHub requires.
func (c *Client) setOrgSecret(ctx context.Context, org, name, value string, access orgAccess) error {
	key, _, err := c.gh.Actions.GetOrgPublicKey(ctx, org)
	if err != nil {
		return convertError(err)
	}
	sealed, err := seal(key.GetKey(), value)
	if err != nil {
		return err
	}
	_, err = c.gh.Actions.CreateOrUpdateOrgSecret(ctx, org, name, gh.SecretOrgRequest{
		KeyID: key.GetKeyID(), EncryptedValue: sealed, Visibility: access.visibility, SelectedRepositoryIDs: access.repoIDs,
	})
	return convertError(err)
}

// selectedRepos lists the repositories selected for an organisation secret.
func (c *Client) selectedRepos(ctx context.Context, org, name string) ([]*gh.Repository, error) {
	var repos []*gh.Repository
	opts := gh.ListOptions{PerPage: 100}
	for {
		list, resp, err := c.gh.Actions.ListSelectedReposForOrgSecret(ctx, org, name, &opts)
		if err != nil {
			return nil, convertError(err)
		}
		repos = append(repos, list.Repositories...)
		if resp.NextPage == 0 {
			return repos, nil
		}
		opts.Page = resp.NextPage
	}
}

// keptOrgAccess is what storing a secret's new value must send to leave its
// access as it is: all of it, for a new secret.
func (c *Client) keptOrgAccess(ctx context.Context, org, name string) (orgAccess, error) {
	cur, _, err := c.gh.Actions.GetOrgSecret(ctx, org, name)
	if err = convertError(err); gitoperationslib.IsNotFound(err) {
		return orgAccess{visibility: orgVisibility}, nil
	}
	if err != nil {
		return orgAccess{}, err
	}
	access := orgAccess{visibility: cur.Visibility}
	if cur.Visibility == string(gitoperationslib.SecretVisibleSelected) {
		repos, err := c.selectedRepos(ctx, org, name)
		if err != nil {
			return orgAccess{}, err
		}
		for _, r := range repos {
			access.repoIDs = append(access.repoIDs, r.GetID())
		}
	}
	return access, nil
}

// repoIDs finds the IDs of an organisation's repositories, which is how GitHub
// addresses them here. An empty list stays non-nil, so that it is sent.
func (c *Client) repoIDs(ctx context.Context, org string, repos []string) ([]int64, error) {
	ids := make([]int64, len(repos))
	for i, name := range repos {
		r, _, err := c.gh.Repositories.Get(ctx, org, name)
		if err != nil {
			return nil, convertError(err)
		}
		ids[i] = r.GetID()
	}
	return ids, nil
}

// GetOrgSecretAccess implements gitoperationslib.OrgSecrets.
func (c *Client) GetOrgSecretAccess(ctx context.Context, org, name string) (*gitoperationslib.SecretAccess, error) {
	if org == "" || name == "" {
		return nil, errors.New("github: secrets.get_access: organisation and name are required")
	}
	cur, _, err := c.gh.Actions.GetOrgSecret(ctx, org, name)
	if err != nil {
		return nil, convertError(err)
	}
	access := &gitoperationslib.SecretAccess{Visibility: gitoperationslib.SecretVisibility(cur.Visibility)}
	if access.Visibility == gitoperationslib.SecretVisibleSelected {
		repos, err := c.selectedRepos(ctx, org, name)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			access.Repos = append(access.Repos, r.GetName())
		}
	}
	return access, nil
}

// SetOrgSecret implements gitoperationslib.OrgSecrets.
func (c *Client) SetOrgSecret(ctx context.Context, org, name, value string, access gitoperationslib.SecretAccess) error {
	if org == "" || name == "" {
		return errors.New("github: secrets.set: organisation and name are required")
	}
	out := orgAccess{visibility: string(access.Visibility)}
	switch access.Visibility {
	case gitoperationslib.SecretVisibleAll, gitoperationslib.SecretVisiblePrivate:
		if len(access.Repos) > 0 {
			return fmt.Errorf("github: secrets.set: repositories only apply to the visibility %q", gitoperationslib.SecretVisibleSelected)
		}
	case gitoperationslib.SecretVisibleSelected:
		var err error
		if out.repoIDs, err = c.repoIDs(ctx, org, access.Repos); err != nil {
			return err
		}
	default:
		return fmt.Errorf("github: secrets.set: invalid visibility %q", access.Visibility)
	}
	return c.setOrgSecret(ctx, org, name, value, out)
}

// SetOrgSecretRepos implements gitoperationslib.OrgSecrets.
func (c *Client) SetOrgSecretRepos(ctx context.Context, org, name string, repos []string) error {
	if org == "" || name == "" {
		return errors.New("github: secrets.set_repos: organisation and name are required")
	}
	ids, err := c.repoIDs(ctx, org, repos)
	if err != nil {
		return err
	}
	_, err = c.gh.Actions.SetSelectedReposForOrgSecret(ctx, org, name, ids)
	return convertError(err)
}
