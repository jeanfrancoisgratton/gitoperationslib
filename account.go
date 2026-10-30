package gitoperationslib

import (
	"context"
	"io"
)

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
	Profile() Profile
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
	// of the token. keyID is GPGKey.KeyID. Forgejo only. Forgejo 15 cannot
	// verify a key whose ID starts with 0 (HTTP 500, matching
	// ErrNotSupported): delete such a key and Add it again with the
	// signature instead.
	Verify(ctx context.Context, keyID, armoredSignature string) (*GPGKey, error)
}

// Profile edits the authenticated user's profile. Forgejo and GitHub
// implement it; Bitbucket has no API for it and returns ErrNotSupported.
type Profile interface {
	// Edit changes the profile and returns the updated user. A field the
	// backend cannot change fails the whole call with ErrNotSupported,
	// before any request is sent.
	Edit(ctx context.Context, opts EditProfileOptions) (*User, error)
	// SetAvatar uploads image (PNG, JPEG, GIF…; at most MaxAvatarSize
	// bytes) as the avatar. The server checks the image: Forgejo answers
	// HTTP 500 for an unknown format or an image wider or taller than 4096
	// pixels. Forgejo only.
	SetAvatar(ctx context.Context, image io.Reader) error
	// DeleteAvatar restores the default avatar. Forgejo only.
	DeleteAvatar(ctx context.Context) error
}

// MaxAvatarSize is the largest image SetAvatar sends; larger ones are
// refused before any request.
const MaxAvatarSize = 10 << 20

// EditProfileOptions changes the authenticated user's profile. Nil fields
// are left unchanged; an empty string clears a field.
type EditProfileOptions struct {
	FullName    *string `json:"fullName,omitzero"`
	Description *string `json:"description,omitzero"` // GitHub: bio
	Website     *string `json:"website,omitzero"`     // GitHub: blog
	Location    *string `json:"location,omitzero"`

	// Forgejo only.
	Pronouns *string       `json:"pronouns,omitzero"`
	Language *string       `json:"language,omitzero"` // interface language, e.g. "fr-FR"
	Settings *EditSettings `json:"settings,omitzero"`

	// GitHub only.
	Company *string `json:"company,omitzero"`
	// PublicEmail is the address shown on the profile: one of the verified
	// addresses, or "" to show none.
	PublicEmail     *string `json:"publicEmail,omitzero"`
	Hireable        *bool   `json:"hireable,omitzero"`
	TwitterUsername *string `json:"twitterUsername,omitzero"`
}

// EditSettings changes a Forgejo user's preferences (see UserSettings). Nil
// fields are left unchanged. Forgejo stores any Theme value, even one the
// instance does not offer.
type EditSettings struct {
	Theme               *string `json:"theme,omitzero"`
	DiffViewStyle       *string `json:"diffViewStyle,omitzero"` // "unified" or "split"
	HideEmail           *bool   `json:"hideEmail,omitzero"`
	HideActivity        *bool   `json:"hideActivity,omitzero"`
	HidePronouns        *bool   `json:"hidePronouns,omitzero"`
	EnableRepoUnitHints *bool   `json:"enableRepoUnitHints,omitzero"`
}
