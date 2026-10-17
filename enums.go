package gitoperationslib

// Backend identifies which forge implementation a Client talks to.
type Backend string

const (
	BackendForgejo Backend = "forgejo"
	BackendGitHub  Backend = "github"
)

// Visibility is the visibility of a repository.
// The zero value means "use the backend default" in option structs.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
	// VisibilityInternal is visible to all members of a GitHub enterprise.
	// Forgejo repositories only have a private flag, so the Forgejo backend
	// is expected to return ErrNotSupported for it. [verify in increment 3]
	VisibilityInternal Visibility = "internal"
)

// Valid reports whether v is one of the named visibilities.
func (v Visibility) Valid() bool {
	switch v {
	case VisibilityPublic, VisibilityPrivate, VisibilityInternal:
		return true
	}
	return false
}

// MergeMethod is the strategy used to merge a pull request.
// Values match Forgejo's "Do" field; the GitHub backend supports only
// MergeMethodMerge, MergeMethodSquash and MergeMethodRebase.
type MergeMethod string

const (
	MergeMethodMerge           MergeMethod = "merge"
	MergeMethodRebase          MergeMethod = "rebase"
	MergeMethodRebaseMerge     MergeMethod = "rebase-merge"
	MergeMethodSquash          MergeMethod = "squash"
	MergeMethodFastForwardOnly MergeMethod = "fast-forward-only"
	MergeMethodManuallyMerged  MergeMethod = "manually-merged"
)

// Valid reports whether m is one of the named merge methods.
func (m MergeMethod) Valid() bool {
	switch m {
	case MergeMethodMerge, MergeMethodRebase, MergeMethodRebaseMerge,
		MergeMethodSquash, MergeMethodFastForwardOnly, MergeMethodManuallyMerged:
		return true
	}
	return false
}
