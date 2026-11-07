package forgejo

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var (
	_ gitoperationslib.OrgLabels  = (*Client)(nil)
	_ gitoperationslib.LabelsByID = (*Client)(nil)
)

func orgLabelsPath(org string) string { return forgejoapi.Path("orgs", org, "labels") }

// ListOrgLabels asks who org is only after a 404, which the server answers for a
// user and for a name that does not exist alike.
func (c *Client) ListOrgLabels(ctx context.Context, org string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
	var out []labelJSON
	resp, err := c.api.Do(ctx, http.MethodGet, orgLabelsPath(org), forgejoapi.ListQuery(opts), nil, &out)
	if gitoperationslib.IsNotFound(err) {
		if _, userErr := c.api.Do(ctx, http.MethodGet, forgejoapi.Path("users", org), nil, nil, nil); userErr == nil {
			return gitoperationslib.Page[gitoperationslib.Label]{Items: []gitoperationslib.Label{}}, nil
		}
	}
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Label]{}, err
	}
	return toPage(resp, out, (*labelJSON).toLabel), nil
}

func (c *Client) GetOrgLabel(ctx context.Context, org string, id int64) (*gitoperationslib.Label, error) {
	return c.labelAt(ctx, orgLabelsPath(org), id)
}

func (c *Client) CreateOrgLabel(ctx context.Context, org string, label gitoperationslib.Label) (*gitoperationslib.Label, error) {
	return c.createLabelAt(ctx, orgLabelsPath(org), label)
}

func (c *Client) EditOrgLabel(ctx context.Context, org string, id int64, opts gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	return c.editLabelAt(ctx, orgLabelsPath(org), id, opts)
}

func (c *Client) DeleteOrgLabel(ctx context.Context, org string, id int64) error {
	return c.deleteLabelAt(ctx, orgLabelsPath(org), id)
}

func (c *Client) EditLabelByID(ctx context.Context, owner, repo string, id int64, opts gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	return c.editLabelAt(ctx, repoPath(owner, repo, "labels"), id, opts)
}

func (c *Client) DeleteLabelByID(ctx context.Context, owner, repo string, id int64) error {
	return c.deleteLabelAt(ctx, repoPath(owner, repo, "labels"), id)
}

func (c *Client) labelAt(ctx context.Context, base string, id int64) (*gitoperationslib.Label, error) {
	var out labelJSON
	if _, err := c.api.Do(ctx, http.MethodGet, base+"/"+strconv.FormatInt(id, 10), nil, nil, &out); err != nil {
		return nil, err
	}
	label := out.toLabel()
	return &label, nil
}

// deleteLabelAt reads the label first: the server answers 204 for one that does
// not exist.
func (c *Client) deleteLabelAt(ctx context.Context, base string, id int64) error {
	if _, err := c.labelAt(ctx, base, id); err != nil {
		return err
	}
	_, err := c.api.Do(ctx, http.MethodDelete, base+"/"+strconv.FormatInt(id, 10), nil, nil, nil)
	return err
}
