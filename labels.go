package gitoperationslib

import (
	"context"
)

// Labels manages repository labels, addressed by name. EditByID and DeleteByID
// address them by Label.ID, to repair names several labels share
// (ErrAmbiguousName), which Forgejo allows (CapLabelsByID; GitHub refuses such
// names).
type Labels interface {
	List(ctx context.Context, owner, repo string, opts ListOptions) (Page[Label], error)
	Get(ctx context.Context, owner, repo, name string) (*Label, error)
	Create(ctx context.Context, owner, repo string, label Label) (*Label, error)
	Edit(ctx context.Context, owner, repo, name string, opts EditLabelOptions) (*Label, error)
	Delete(ctx context.Context, owner, repo, name string) error

	EditByID(ctx context.Context, owner, repo string, id int64, opts EditLabelOptions) (*Label, error)
	DeleteByID(ctx context.Context, owner, repo string, id int64) error
}

// Label is a repository's or an organisation's label.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"` // six hex digits, lower case, without "#"
	Description string `json:"description"`
}

// EditLabelOptions changes a label. Nil fields are left unchanged.
type EditLabelOptions struct {
	Name        *string `json:"name,omitzero"` // renames the label; ErrNameTaken if in use
	Color       *string `json:"color,omitzero"`
	Description *string `json:"description,omitzero"`
}

// OrgLabels manages an organisation's labels, which all its repositories can use
// by name, through Orgs().Labels(). Forgejo only (CapOrgLabels): GitHub's
// organisation default labels only seed new repositories, and Bitbucket has no
// issues. They are addressed by Label.ID, as names can repeat.
type OrgLabels interface {
	// List returns an empty page when org is a user, who has none, and
	// a 404 *APIError when it is neither.
	List(ctx context.Context, org string, opts ListOptions) (Page[Label], error)
	Get(ctx context.Context, org string, id int64) (*Label, error)
	// Create returns ErrNameTaken if the organisation has that name.
	Create(ctx context.Context, org string, label Label) (*Label, error)
	Edit(ctx context.Context, org string, id int64, opts EditLabelOptions) (*Label, error)
	Delete(ctx context.Context, org string, id int64) error
}
