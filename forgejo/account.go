package forgejo

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/refusal"
)

var _ gitoperationslib.Account = (*Client)(nil)

func (c *Client) Emails() gitoperationslib.Emails   { return emails{c} }
func (c *Client) SSHKeys() gitoperationslib.SSHKeys { return sshKeys{c} }
func (c *Client) GPGKeys() gitoperationslib.GPGKeys { return gpgKeys{c} }

type emails struct{ c *Client }

func toEmails(list []emailJSON) []gitoperationslib.Email {
	out := make([]gitoperationslib.Email, len(list))
	for i, e := range list {
		out[i] = gitoperationslib.Email{Address: e.Email, Primary: e.Primary, Verified: e.Verified}
	}
	return out
}

func (e emails) List(ctx context.Context) ([]gitoperationslib.Email, error) {
	var out []emailJSON
	if _, err := e.c.api.Do(ctx, http.MethodGet, "user/emails", nil, nil, &out); err != nil {
		return nil, err
	}
	return toEmails(out), nil
}

func (e emails) Add(ctx context.Context, addresses ...string) ([]gitoperationslib.Email, error) {
	if len(addresses) == 0 {
		return nil, errors.New("forgejo: emails.add: no address given")
	}
	var out []emailJSON
	if _, err := e.c.api.Do(ctx, http.MethodPost, "user/emails", nil, map[string][]string{"emails": addresses}, &out); err != nil {
		return nil, err
	}
	return toEmails(out), nil
}

func (e emails) Delete(ctx context.Context, addresses ...string) error {
	if len(addresses) == 0 {
		return errors.New("forgejo: emails.delete: no address given")
	}
	_, err := e.c.api.Do(ctx, http.MethodDelete, "user/emails", nil, map[string][]string{"emails": addresses}, nil)
	return err
}

type sshKeys struct{ c *Client }

func sshKeyPath(id int64) string { return forgejoapi.Path("user", "keys", strconv.FormatInt(id, 10)) }

func (k sshKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.SSHKey], error) {
	return listAt(k.c, "user/keys", (*sshKeyJSON).toSSHKey)(ctx, opts)
}

func (k sshKeys) Get(ctx context.Context, id int64) (*gitoperationslib.SSHKey, error) {
	var out sshKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodGet, sshKeyPath(id), nil, nil, &out); err != nil {
		return nil, err
	}
	key := out.toSSHKey()
	return &key, nil
}

func (k sshKeys) Add(ctx context.Context, opts gitoperationslib.AddSSHKeyOptions) (*gitoperationslib.SSHKey, error) {
	if opts.Title == "" || opts.Key == "" {
		return nil, errors.New("forgejo: sshkeys.add: title and key are required")
	}
	body := map[string]any{"title": opts.Title, "key": opts.Key, "read_only": opts.ReadOnly}
	var out sshKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodPost, "user/keys", nil, body, &out); err != nil {
		return nil, err
	}
	key := out.toSSHKey()
	return &key, nil
}

func (k sshKeys) Delete(ctx context.Context, id int64) error {
	_, err := k.c.api.Do(ctx, http.MethodDelete, sshKeyPath(id), nil, nil, nil)
	return err
}

type gpgKeys struct{ c *Client }

func gpgKeyPath(id int64) string {
	return forgejoapi.Path("user", "gpg_keys", strconv.FormatInt(id, 10))
}

// gpgRefusals recognise Forgejo's requests for a signature: 404 when none of
// the key's e-mail addresses belongs to the account, 422 when the signature
// does not match the token (both checked on Forgejo 15.0.11).
var gpgRefusals = []refusal.Rule{
	{Status: http.StatusNotFound, Fragment: "provide a valid signature for the token", Sentinel: gitoperationslib.ErrGPGSignature},
	{Status: http.StatusUnprocessableEntity, Fragment: "provide a valid signature for the token", Sentinel: gitoperationslib.ErrGPGSignature},
}

func (k gpgKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.GPGKey], error) {
	return listAt(k.c, "user/gpg_keys", (*gpgKeyJSON).toGPGKey)(ctx, opts)
}

func (k gpgKeys) Get(ctx context.Context, id int64) (*gitoperationslib.GPGKey, error) {
	var out gpgKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodGet, gpgKeyPath(id), nil, nil, &out); err != nil {
		return nil, err
	}
	key := out.toGPGKey()
	return &key, nil
}

func (k gpgKeys) Add(ctx context.Context, armoredPublicKey, armoredSignature string) (*gitoperationslib.GPGKey, error) {
	if armoredPublicKey == "" {
		return nil, errors.New("forgejo: gpgkeys.add: the armored public key is required")
	}
	body := map[string]string{"armored_public_key": armoredPublicKey}
	if armoredSignature != "" {
		body["armored_signature"] = armoredSignature
	}
	var out gpgKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodPost, "user/gpg_keys", nil, body, &out); err != nil {
		return nil, refusal.Mark(err, gpgRefusals...)
	}
	key := out.toGPGKey()
	return &key, nil
}

func (k gpgKeys) Delete(ctx context.Context, id int64) error {
	_, err := k.c.api.Do(ctx, http.MethodDelete, gpgKeyPath(id), nil, nil, nil)
	return err
}

func (k gpgKeys) VerificationToken(ctx context.Context) (string, error) {
	return k.c.api.Text(ctx, "user/gpg_key_token")
}

func (k gpgKeys) Verify(ctx context.Context, keyID, armoredSignature string) (*gitoperationslib.GPGKey, error) {
	if keyID == "" || armoredSignature == "" {
		return nil, errors.New("forgejo: gpgkeys.verify: key ID and signature are required")
	}
	body := map[string]string{"key_id": keyID, "armored_signature": armoredSignature}
	var out gpgKeyJSON
	if _, err := k.c.api.Do(ctx, http.MethodPost, "user/gpg_key_verify", nil, body, &out); err != nil {
		return nil, refusal.Mark(err, gpgRefusals...)
	}
	key := out.toGPGKey()
	return &key, nil
}
