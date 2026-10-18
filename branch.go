package gitoperationslib

// Branch is a repository branch.
type Branch struct {
	Name      string
	SHA       string // head commit
	Protected bool
	IsDefault bool
}

// CreateBranchOptions describes a branch to create.
type CreateBranchOptions struct {
	Name string // required
	// From is the branch, tag or commit SHA to start from; empty means the
	// default branch.
	From string
}

// Tag is a repository tag.
type Tag struct {
	Name string
	SHA  string // commit the tag points to (annotated tags are peeled)
	// Message is the annotation of an annotated tag; empty for lightweight
	// tags. GitHub reports it from Get only, not from List.
	Message    string
	TarballURL string
	ZipballURL string
}

// CreateTagOptions describes a tag to create. To publish a release for a
// tag, use Releases().Create, which also creates the tag when needed.
type CreateTagOptions struct {
	Name string // required
	// Target is the branch, tag or commit SHA to tag; empty means the
	// default branch.
	Target string
	// Message makes an annotated tag; empty makes a lightweight tag.
	Message string
}
