package gitoperationslib

// Branch is a repository branch.
type Branch struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"` // head commit
	Protected bool   `json:"protected"`
	IsDefault bool   `json:"isDefault"`
}

// CreateBranchOptions describes a branch to create.
type CreateBranchOptions struct {
	Name string `json:"name"` // required
	// From is the branch, tag or commit SHA to start from; empty means the
	// default branch.
	From string `json:"from,omitzero"`
}

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
