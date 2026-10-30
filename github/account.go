package github

import (
	"context"
	"errors"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.Account = (*Client)(nil)

// Emails, SSHKeys and GPGKeys need these token permissions: classic tokens
// the user, admin:public_key and admin:gpg_key scopes; fine-grained tokens
// the account permissions "Email addresses", "Git SSH keys" and "GPG keys".
func (c *Client) Emails() gitoperationslib.Emails   { return emails{c} }
func (c *Client) SSHKeys() gitoperationslib.SSHKeys { return sshKeys{c} }
func (c *Client) GPGKeys() gitoperationslib.GPGKeys { return gpgKeys{c} }

type emails struct{ c *Client }

func (e emails) List(ctx context.Context) ([]gitoperationslib.Email, error) {
	var all []gitoperationslib.Email
	opts := &gh.ListOptions{PerPage: 100}
	for {
		list, resp, err := e.c.gh.Users.ListEmails(ctx, opts)
		if err != nil {
			return nil, convertError(err)
		}
		for _, m := range list {
			all = append(all, toEmail(m))
		}
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

func (e emails) Add(ctx context.Context, addresses ...string) ([]gitoperationslib.Email, error) {
	if len(addresses) == 0 {
		return nil, errors.New("github: emails.add: no address given")
	}
	list, _, err := e.c.gh.Users.AddEmails(ctx, addresses)
	if err != nil {
		return nil, convertError(err)
	}
	out := make([]gitoperationslib.Email, len(list))
	for i, m := range list {
		out[i] = toEmail(m)
	}
	return out, nil
}

func (e emails) Delete(ctx context.Context, addresses ...string) error {
	if len(addresses) == 0 {
		return errors.New("github: emails.delete: no address given")
	}
	_, err := e.c.gh.Users.DeleteEmails(ctx, addresses)
	return convertError(err)
}

type sshKeys struct{ c *Client }

func (k sshKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.SSHKey], error) {
	lo := ghList(opts)
	list, resp, err := k.c.gh.Users.ListKeys(ctx, "", &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.SSHKey]{}, convertError(err)
	}
	return toPage(list, resp, toSSHKey), nil
}

func (k sshKeys) Get(ctx context.Context, id int64) (*gitoperationslib.SSHKey, error) {
	key, _, err := k.c.gh.Users.GetKey(ctx, id)
	if err != nil {
		return nil, convertError(err)
	}
	out := toSSHKey(key)
	return &out, nil
}

func (k sshKeys) Add(ctx context.Context, opts gitoperationslib.AddSSHKeyOptions) (*gitoperationslib.SSHKey, error) {
	if opts.Title == "" || opts.Key == "" {
		return nil, errors.New("github: sshkeys.add: title and key are required")
	}
	if opts.ReadOnly {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "sshkeys.add", "read-only user keys are a Forgejo feature (GitHub has read-only deploy keys only)")
	}
	key, _, err := k.c.gh.Users.CreateKey(ctx, gh.CreateUserKeyRequest{Title: &opts.Title, Key: opts.Key})
	if err != nil {
		return nil, convertError(err)
	}
	out := toSSHKey(key)
	return &out, nil
}

func (k sshKeys) Delete(ctx context.Context, id int64) error {
	_, err := k.c.gh.Users.DeleteKey(ctx, id)
	return convertError(err)
}

type gpgKeys struct{ c *Client }

const noGPGVerify = "GitHub verifies GPG keys through the account's verified e-mail addresses, not by signature"

func (k gpgKeys) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.GPGKey], error) {
	lo := ghList(opts)
	list, resp, err := k.c.gh.Users.ListGPGKeys(ctx, "", &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.GPGKey]{}, convertError(err)
	}
	return toPage(list, resp, toGPGKey), nil
}

func (k gpgKeys) Get(ctx context.Context, id int64) (*gitoperationslib.GPGKey, error) {
	key, _, err := k.c.gh.Users.GetGPGKey(ctx, id)
	if err != nil {
		return nil, convertError(err)
	}
	out := toGPGKey(key)
	return &out, nil
}

func (k gpgKeys) Add(ctx context.Context, armoredPublicKey, armoredSignature string) (*gitoperationslib.GPGKey, error) {
	if armoredPublicKey == "" {
		return nil, errors.New("github: gpgkeys.add: the armored public key is required")
	}
	if armoredSignature != "" {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "gpgkeys.add", noGPGVerify)
	}
	key, _, err := k.c.gh.Users.CreateGPGKey(ctx, armoredPublicKey)
	if err != nil {
		return nil, convertError(err)
	}
	out := toGPGKey(key)
	return &out, nil
}

func (k gpgKeys) Delete(ctx context.Context, id int64) error {
	_, err := k.c.gh.Users.DeleteGPGKey(ctx, id)
	return convertError(err)
}

func (k gpgKeys) VerificationToken(context.Context) (string, error) {
	return "", gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "gpgkeys.verificationtoken", noGPGVerify)
}

func (k gpgKeys) Verify(context.Context, string, string) (*gitoperationslib.GPGKey, error) {
	return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "gpgkeys.verify", noGPGVerify)
}
