package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

var _ gitoperationslib.Account = (*Client)(nil)

// Emails, SSHKeys and GPGKeys act on the authenticated user. E-mail addresses
// can only be listed: Bitbucket refuses to change them with token
// authentication.
func (c *Client) Emails() gitoperationslib.Emails   { return emails{c} }
func (c *Client) SSHKeys() gitoperationslib.SSHKeys { return sshKeys{c} }
func (c *Client) GPGKeys() gitoperationslib.GPGKeys { return gpgKeys{c} }

const noEmailChange = "Bitbucket does not allow changing e-mail addresses with token authentication"

type emails struct{ c *Client }

func (e emails) List(ctx context.Context) ([]gitoperationslib.Email, error) {
	list, err := bitbucketapi.All[emailJSON](ctx, e.c.api, "user/emails", nil)
	if err != nil {
		return nil, err
	}
	out := make([]gitoperationslib.Email, len(list))
	for i, m := range list {
		out[i] = gitoperationslib.Email{Address: m.Email, Primary: m.IsPrimary, Verified: m.IsConfirmed}
	}
	return out, nil
}

func (e emails) Add(context.Context, ...string) ([]gitoperationslib.Email, error) {
	return nil, notSupported("emails.add", noEmailChange)
}

func (e emails) Delete(context.Context, ...string) error {
	return notSupported("emails.delete", noEmailChange)
}

// userKeysPath is /users/{uuid}/<kind>[/rest…] for the authenticated user.
func (c *Client) userKeysPath(ctx context.Context, kind string, rest ...string) (string, error) {
	u, err := c.currentUser(ctx)
	if err != nil {
		return "", err
	}
	return bitbucketapi.Path(append([]string{"users", u.UUID, kind}, rest...)...), nil
}

// notFound is the 404 returned when no key has the derived ID.
func notFound(what string) error {
	return &gitoperationslib.APIError{Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound, Message: what + " not found"}
}

type sshKeys struct{ c *Client }

func (k sshKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.SSHKey], error) {
	path, err := k.c.userKeysPath(ctx, "ssh-keys")
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.SSHKey]{}, err
	}
	page, err := bitbucketapi.List[sshKeyJSON](ctx, k.c.api, path, bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.SSHKey]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.SSHKey]{Items: make([]gitoperationslib.SSHKey, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toSSHKey()
	}
	return out, nil
}

// find returns the key whose derived ID is id.
func (k sshKeys) find(ctx context.Context, id int64) (*sshKeyJSON, error) {
	path, err := k.c.userKeysPath(ctx, "ssh-keys")
	if err != nil {
		return nil, err
	}
	all, err := bitbucketapi.All[sshKeyJSON](ctx, k.c.api, path, nil)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if hexID(all[i].UUID) == id {
			return &all[i], nil
		}
	}
	return nil, notFound("SSH key")
}

func (k sshKeys) Get(ctx context.Context, id int64) (*gitoperationslib.SSHKey, error) {
	j, err := k.find(ctx, id)
	if err != nil {
		return nil, err
	}
	key := j.toSSHKey()
	return &key, nil
}

func (k sshKeys) Add(ctx context.Context, opts gitoperationslib.AddSSHKeyOptions) (*gitoperationslib.SSHKey, error) {
	if opts.Title == "" || opts.Key == "" {
		return nil, errors.New("bitbucket: sshkeys.add: title and key are required")
	}
	if opts.ReadOnly {
		return nil, notSupported("sshkeys.add", "read-only user keys are a Forgejo feature")
	}
	path, err := k.c.userKeysPath(ctx, "ssh-keys")
	if err != nil {
		return nil, err
	}
	var out sshKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodPost, path, nil, map[string]string{"key": opts.Key, "label": opts.Title}, &out); err != nil {
		return nil, err
	}
	key := out.toSSHKey()
	return &key, nil
}

func (k sshKeys) Delete(ctx context.Context, id int64) error {
	j, err := k.find(ctx, id)
	if err != nil {
		return err
	}
	path, err := k.c.userKeysPath(ctx, "ssh-keys", j.UUID)
	if err != nil {
		return err
	}
	_, err = k.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

// gpgKeyJSON is a Bitbucket GPG key. Bitbucket reports neither e-mail
// addresses nor capabilities.
type gpgKeyJSON struct {
	KeyID       string       `json:"key_id"`
	Fingerprint string       `json:"fingerprint"`
	Key         string       `json:"key"`
	CreatedOn   time.Time    `json:"created_on"`
	ExpiresOn   *time.Time   `json:"expires_on"`
	Subkeys     []gpgKeyJSON `json:"subkeys"`
}

func (k *gpgKeyJSON) toGPGKey() gitoperationslib.GPGKey {
	key := gitoperationslib.GPGKey{
		ID: hexID(k.Fingerprint), KeyID: strings.ToUpper(k.KeyID), PublicKey: k.Key, CreatedAt: k.CreatedOn,
	}
	if k.ExpiresOn != nil {
		key.ExpiresAt = *k.ExpiresOn
	}
	for i := range k.Subkeys {
		key.Subkeys = append(key.Subkeys, k.Subkeys[i].toGPGKey())
	}
	return key
}

const noGPGVerify = "Bitbucket checks a GPG key's e-mail addresses against the account's, not a signature"

type gpgKeys struct{ c *Client }

func (k gpgKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.GPGKey], error) {
	path, err := k.c.userKeysPath(ctx, "gpg-keys")
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.GPGKey]{}, err
	}
	page, err := bitbucketapi.List[gpgKeyJSON](ctx, k.c.api, path, bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.GPGKey]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.GPGKey]{Items: make([]gitoperationslib.GPGKey, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toGPGKey()
	}
	return out, nil
}

func (k gpgKeys) find(ctx context.Context, id int64) (*gpgKeyJSON, error) {
	path, err := k.c.userKeysPath(ctx, "gpg-keys")
	if err != nil {
		return nil, err
	}
	all, err := bitbucketapi.All[gpgKeyJSON](ctx, k.c.api, path, nil)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if hexID(all[i].Fingerprint) == id {
			return &all[i], nil
		}
	}
	return nil, notFound("GPG key")
}

func (k gpgKeys) Get(ctx context.Context, id int64) (*gitoperationslib.GPGKey, error) {
	j, err := k.find(ctx, id)
	if err != nil {
		return nil, err
	}
	key := j.toGPGKey()
	return &key, nil
}

// Add uploads a key. Bitbucket refuses (HTTP 400) a key with none of the
// account's e-mail addresses.
func (k gpgKeys) Add(ctx context.Context, armoredPublicKey, armoredSignature string) (*gitoperationslib.GPGKey, error) {
	if armoredPublicKey == "" {
		return nil, errors.New("bitbucket: gpgkeys.add: the armored public key is required")
	}
	if armoredSignature != "" {
		return nil, notSupported("gpgkeys.add", noGPGVerify)
	}
	path, err := k.c.userKeysPath(ctx, "gpg-keys")
	if err != nil {
		return nil, err
	}
	var out gpgKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodPost, path, nil, map[string]string{"key": armoredPublicKey}, &out); err != nil {
		return nil, err
	}
	key := out.toGPGKey()
	return &key, nil
}

func (k gpgKeys) Delete(ctx context.Context, id int64) error {
	j, err := k.find(ctx, id)
	if err != nil {
		return err
	}
	path, err := k.c.userKeysPath(ctx, "gpg-keys", j.Fingerprint)
	if err != nil {
		return err
	}
	_, err = k.c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

func (k gpgKeys) VerificationToken(context.Context) (string, error) {
	return "", notSupported("gpgkeys.verificationtoken", noGPGVerify)
}

func (k gpgKeys) Verify(context.Context, string, string) (*gitoperationslib.GPGKey, error) {
	return nil, notSupported("gpgkeys.verify", noGPGVerify)
}

// Profile is not available: Bitbucket has no API to change a profile or
// avatar (the profile is the Atlassian account's).
func (c *Client) Profile() gitoperationslib.Profile { return profile{} }

type profile struct{}

const noProfile = "Bitbucket has no API to change the profile or avatar (they belong to the Atlassian account)"

func (profile) Edit(context.Context, gitoperationslib.EditProfileOptions) (*gitoperationslib.User, error) {
	return nil, notSupported("profile.edit", noProfile)
}

func (profile) SetAvatar(context.Context, io.Reader) error {
	return notSupported("profile.setavatar", noProfile)
}

func (profile) DeleteAvatar(context.Context) error {
	return notSupported("profile.deleteavatar", noProfile)
}

// Tokens is not available: personal access tokens cannot be created or
// revoked through the API.
func (c *Client) Tokens() gitoperationslib.Tokens { return tokens{} }

type tokens struct{}

const noTokens = "personal access tokens cannot be created or revoked through the API"

func (tokens) List(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.AccessToken], error) {
	return gitoperationslib.Page[gitoperationslib.AccessToken]{}, notSupported("tokens.list", noTokens)
}

func (tokens) Create(context.Context, gitoperationslib.CreateTokenOptions) (*gitoperationslib.AccessToken, error) {
	return nil, notSupported("tokens.create", noTokens)
}

func (tokens) Delete(context.Context, int64) error { return notSupported("tokens.delete", noTokens) }

func (tokens) DeleteByName(context.Context, string) error {
	return notSupported("tokens.delete", noTokens)
}
