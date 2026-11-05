package gitoperationslib

import (
	"context"
	"errors"
	"time"
)

// Mirrors manages a repository's mirrors, which takes admin access to it. The
// Forgejo client implements it (CapMirrors); find it with a type assertion:
//
//	m, ok := client.(gitoperationslib.Mirrors)
//
// A push mirror sends the repository to another address. A pull mirror is a
// repository that copies another one, and Repo.Mirror reports it; the library
// cannot create one, as that takes a migration.
type Mirrors interface {
	ListPushMirrors(ctx context.Context, owner, repo string, opts ListOptions) (Page[PushMirror], error)
	// GetPushMirror finds a push mirror by PushMirror.Name.
	GetPushMirror(ctx context.Context, owner, repo, name string) (*PushMirror, error)
	// CreatePushMirror adds a push mirror; the server chooses its name. It
	// does not contact the remote, so a wrong address or password only shows
	// in PushMirror.LastError after a sync. A remote the server refuses, for
	// instance a host that does not resolve, gives HTTP 400.
	CreatePushMirror(ctx context.Context, owner, repo string, opts CreatePushMirrorOptions) (*PushMirror, error)
	DeletePushMirror(ctx context.Context, owner, repo, name string) error
	// SyncPushMirrors pushes to all of the repository's push mirrors, and
	// returns when that is done; Forgejo cannot sync just one. The first that
	// fails gives HTTP 500 naming it, with the reason in its
	// PushMirror.LastError, and those after it are not pushed. A branch can
	// take a moment to show on a remote that is a forge.
	SyncPushMirrors(ctx context.Context, owner, repo string) error
	// SyncMirror queues a fetch for a pull mirror and returns at once. It is
	// HTTP 400 for a repository that is not a mirror.
	SyncMirror(ctx context.Context, owner, repo string) error
}

// PushMirror is a remote a repository is pushed to.
type PushMirror struct {
	// Name identifies the mirror within its repository.
	Name string `json:"name"`
	// RemoteAddress never holds credentials. Forgejo rewrites an SSH address
	// as ssh://host/path, without the user.
	RemoteAddress string `json:"remoteAddress"`
	// BranchFilter limits the branches pushed; empty means all of them.
	BranchFilter string `json:"branchFilter,omitzero"`
	// Interval is the time between pushes; zero means no periodic push.
	Interval     time.Duration `json:"interval,omitzero"`
	SyncOnCommit bool          `json:"syncOnCommit,omitzero"`
	// PublicKey is set on a mirror using SSH: the remote must accept this key
	// for writing, for instance as a deploy key.
	PublicKey string `json:"publicKey,omitzero"`
	// LastError is what went wrong with the last push.
	LastError string `json:"lastError,omitzero"`
	// LastSync is when the last push was tried, whether or not it worked: see
	// LastError. It is zero until the first one.
	LastSync  time.Time `json:"lastSync,omitzero"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
}

// CreatePushMirrorOptions describes a push mirror. RemoteAddress is required.
type CreatePushMirrorOptions struct {
	// RemoteAddress must not hold credentials: the server refuses it.
	RemoteAddress string `json:"remoteAddress"`
	// Username and Password authenticate to an HTTP remote; both are stored
	// by the server and never returned. They are incompatible with UseSSH.
	Username string `json:"username,omitzero"`
	Password string `json:"-"`
	// UseSSH makes the server generate a key: see PushMirror.PublicKey.
	UseSSH bool `json:"useSsh,omitzero"`
	// Interval is the time between pushes; zero means no periodic push. The
	// server refuses a shorter one than its minimum (by default ten minutes).
	Interval time.Duration `json:"interval,omitzero"`
	// SyncOnCommit also pushes after each commit.
	SyncOnCommit bool `json:"syncOnCommit,omitzero"`
	// BranchFilter limits the branches pushed, as a comma-separated list of
	// names or patterns (main,release/*); empty means all of them.
	BranchFilter string `json:"branchFilter,omitzero"`
}

// Validate reports a missing or contradictory field.
func (o CreatePushMirrorOptions) Validate() error {
	switch {
	case o.RemoteAddress == "":
		return errors.New("remote address is required")
	case o.UseSSH && (o.Username != "" || o.Password != ""):
		return errors.New("UseSSH excludes Username and Password")
	case o.Interval < 0:
		return errors.New("interval cannot be negative")
	}
	return nil
}
