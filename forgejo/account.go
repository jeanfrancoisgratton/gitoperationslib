package forgejo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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
		return nil, leadingZeroBug(keyID, refusal.Mark(err, gpgRefusals...))
	}
	key := out.toGPGKey()
	return &key, nil
}

func (c *Client) Profile() gitoperationslib.Profile { return profile{c} }

type profile struct{ c *Client }

// Edit sends the changes to /user/settings, then reads the user back.
func (p profile) Edit(ctx context.Context, opts gitoperationslib.EditProfileOptions) (*gitoperationslib.User, error) {
	githubOnly := []struct {
		name string
		set  bool
	}{
		{"company", opts.Company != nil}, {"public e-mail", opts.PublicEmail != nil},
		{"hireable", opts.Hireable != nil}, {"Twitter username", opts.TwitterUsername != nil},
	}
	for _, f := range githubOnly {
		if f.set {
			return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "profile.edit", f.name+" is a GitHub profile field")
		}
	}
	body := map[string]any{}
	for key, v := range map[string]*string{
		"full_name": opts.FullName, "description": opts.Description, "website": opts.Website,
		"location": opts.Location, "pronouns": opts.Pronouns, "language": opts.Language,
	} {
		if v != nil {
			body[key] = *v
		}
	}
	if s := opts.Settings; s != nil {
		for key, v := range map[string]*string{"theme": s.Theme, "diff_view_style": s.DiffViewStyle} {
			if v != nil {
				body[key] = *v
			}
		}
		for key, v := range map[string]*bool{
			"hide_email": s.HideEmail, "hide_activity": s.HideActivity,
			"hide_pronouns": s.HidePronouns, "enable_repo_unit_hints": s.EnableRepoUnitHints,
		} {
			if v != nil {
				body[key] = *v
			}
		}
	}
	if len(body) > 0 {
		if _, err := p.c.api.Do(ctx, http.MethodPatch, "user/settings", nil, body, nil); err != nil {
			return nil, err
		}
	}
	var out meJSON
	if _, err := p.c.api.Do(ctx, http.MethodGet, "user", nil, nil, &out); err != nil {
		return nil, err
	}
	u := out.toUser()
	return &u, nil
}

func (p profile) SetAvatar(ctx context.Context, image io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(image, gitoperationslib.MaxAvatarSize+1))
	if err != nil {
		return fmt.Errorf("forgejo: profile.setavatar: read image: %w", err)
	}
	switch {
	case len(data) == 0:
		return errors.New("forgejo: profile.setavatar: empty image")
	case len(data) > gitoperationslib.MaxAvatarSize:
		return fmt.Errorf("forgejo: profile.setavatar: image larger than %d bytes", gitoperationslib.MaxAvatarSize)
	}
	body := map[string]string{"image": base64.StdEncoding.EncodeToString(data)}
	_, err = p.c.api.Do(ctx, http.MethodPost, "user/avatar", nil, body, nil)
	return err
}

func (p profile) DeleteAvatar(ctx context.Context) error {
	_, err := p.c.api.Do(ctx, http.MethodDelete, "user/avatar", nil, nil, nil)
	return err
}

// leadingZeroBug explains Forgejo's failure to verify a key whose ID starts
// with 0 (HTTP 500 "public gpg key does not exist", checked on 15.0.11: the
// signer's key ID loses its leading zero and no longer matches). The error
// then matches ErrNotSupported. Adding the key with the signature works.
func leadingZeroBug(keyID string, err error) error {
	var apiErr *gitoperationslib.APIError
	if strings.HasPrefix(keyID, "0") && errors.As(err, &apiErr) && apiErr.Err == nil &&
		apiErr.StatusCode == http.StatusInternalServerError && strings.Contains(apiErr.Message, "public gpg key does not exist") {
		apiErr.Err = gitoperationslib.ErrNotSupported
		apiErr.Details = append(apiErr.Details, "Forgejo cannot verify a key whose ID starts with 0: delete the key and add it again with the signature")
	}
	return err
}
