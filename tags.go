package gitoperationslib

import (
	"context"
)

// Tag is a repository tag.
type Tag struct {
	Name string `json:"name"`
	SHA  string `json:"sha"` // commit the tag points to (annotated tags are peeled)
	// Message is the annotation of an annotated tag; empty for lightweight
	// tags. GitHub reports it from Get only, not from List.
	Message    string `json:"message,omitzero"`
	TarballURL string `json:"tarballUrl"`
	ZipballURL string `json:"zipballUrl"`
}

// CreateTagOptions describes a tag to create. To publish a release for a
// tag, use Releases().Create, which also creates the tag when needed.
type CreateTagOptions struct {
	Name string `json:"name"` // required
	// Target is the branch, tag or commit SHA to tag; empty means the
	// default branch.
	Target string `json:"target,omitzero"`
	// Message makes an annotated tag; empty makes a lightweight tag.
	Message string `json:"message,omitzero"`
}

// Tags manages repository tags. Releases are managed with Releases().
type Tags interface {
	List(ctx context.Context, owner, repo string, opts ListOptions) (Page[Tag], error)
	Get(ctx context.Context, owner, repo, name string) (*Tag, error)
	Create(ctx context.Context, owner, repo string, opts CreateTagOptions) (*Tag, error)
	Delete(ctx context.Context, owner, repo, name string) error
}
