package gitoperationslib

import "context"

// Account manages the authenticated user's own account. The Forgejo, GitHub
// and Bitbucket clients implement it (CapAccount); find it with a type
// assertion:
//
//	acct, ok := client.(gitoperationslib.Account)
//
// Parts a forge lacks return ErrNotSupported. Directory().Me() reads the same
// e-mail addresses and keys in one call, along with the rest of the account.
type Account interface {
	Emails() Emails
	SSHKeys() SSHKeys
	GPGKeys() GPGKeys
}

// Emails manages the authenticated user's e-mail addresses. Bitbucket only
// lists them.
type Emails interface {
	List(ctx context.Context) ([]Email, error)
	// Add adds addresses and returns them as the server reports them, not
	// yet verified. GitHub sends each a verification e-mail; Forgejo too
	// when the instance requires confirmation.
	Add(ctx context.Context, addresses ...string) ([]Email, error)
	// Delete removes addresses. The primary address cannot be removed.
	Delete(ctx context.Context, addresses ...string) error
}

// SSHKeys manages the authenticated user's public SSH keys (authentication
// keys; GitHub's signing keys are not included).
//
// Bitbucket identifies keys by UUID: SSHKey.ID is derived from it, and Get
// and Delete list the keys to find it.
type SSHKeys interface {
	List(ctx context.Context, opts ListOptions) (Page[SSHKey], error)
	Get(ctx context.Context, id int64) (*SSHKey, error)
	Add(ctx context.Context, opts AddSSHKeyOptions) (*SSHKey, error)
	Delete(ctx context.Context, id int64) error
}

// AddSSHKeyOptions describes an SSH key to add.
type AddSSHKeyOptions struct {
	Title string `json:"title"` // required
	Key   string `json:"key"`   // required: "ssh-ed25519 AAAA… comment"
	// ReadOnly limits the key to reading (Forgejo only; ErrNotSupported
	// elsewhere).
	ReadOnly bool `json:"readOnly,omitzero"`
}

// GPGKeys manages the authenticated user's GPG keys.
//
// On Forgejo a key is "verified" once its owner has proven they hold it, by
// signing a token from VerificationToken. A key whose e-mail addresses are
// all unknown to the account can only be added with such a signature.
// Bitbucket identifies keys by fingerprint: GPGKey.ID is derived from it.
type GPGKeys interface {
	List(ctx context.Context, opts ListOptions) (Page[GPGKey], error)
	Get(ctx context.Context, id int64) (*GPGKey, error)
	// Add uploads an armored public key. armoredSignature is optional: a
	// detached, armored signature of VerificationToken's token, which marks
	// the key verified (Forgejo only; ErrNotSupported elsewhere when set).
	Add(ctx context.Context, armoredPublicKey, armoredSignature string) (*GPGKey, error)
	Delete(ctx context.Context, id int64) error
	// VerificationToken returns the token to sign, e.g. with
	// "echo -n TOKEN | gpg -a --default-key KEYID --detach-sig". It changes
	// over time. Forgejo only.
	VerificationToken(ctx context.Context) (string, error)
	// Verify marks an added key verified, from a detached, armored signature
	// of the token. keyID is GPGKey.KeyID. Forgejo only.
	Verify(ctx context.Context, keyID, armoredSignature string) (*GPGKey, error)
}
