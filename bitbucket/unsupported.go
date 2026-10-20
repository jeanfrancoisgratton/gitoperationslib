package bitbucket

import (
	"context"
	"io"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Areas Bitbucket Cloud does not have. Every method returns ErrNotSupported.

const (
	noReleases      = "Bitbucket Cloud has no releases (only a repository-wide Downloads area)"
	noTagProtection = "Bitbucket Cloud has no tag protection"
	noIssues        = "the Bitbucket Cloud issue tracker has been retired (CHANGE-3071)"
)

// Releases implements gitoperationslib.Client; every method returns
// ErrNotSupported.
func (c *Client) Releases() gitoperationslib.Releases { return releases{} }

type releases struct{}

func (releases) List(context.Context, string, string, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Release], error) {
	return gitoperationslib.Page[gitoperationslib.Release]{}, notSupported("releases.list", noReleases)
}
func (releases) Get(context.Context, string, string, int64) (*gitoperationslib.Release, error) {
	return nil, notSupported("releases.get", noReleases)
}
func (releases) GetByTag(context.Context, string, string, string) (*gitoperationslib.Release, error) {
	return nil, notSupported("releases.get_by_tag", noReleases)
}
func (releases) Latest(context.Context, string, string) (*gitoperationslib.Release, error) {
	return nil, notSupported("releases.latest", noReleases)
}
func (releases) Create(context.Context, string, string, gitoperationslib.CreateReleaseOptions) (*gitoperationslib.Release, error) {
	return nil, notSupported("releases.create", noReleases)
}
func (releases) Edit(context.Context, string, string, int64, gitoperationslib.EditReleaseOptions) (*gitoperationslib.Release, error) {
	return nil, notSupported("releases.edit", noReleases)
}
func (releases) Delete(context.Context, string, string, int64) error {
	return notSupported("releases.delete", noReleases)
}
func (releases) ListAssets(context.Context, string, string, int64, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Asset], error) {
	return gitoperationslib.Page[gitoperationslib.Asset]{}, notSupported("releases.list_assets", noReleases)
}
func (releases) GetAsset(context.Context, string, string, int64, int64) (*gitoperationslib.Asset, error) {
	return nil, notSupported("releases.get_asset", noReleases)
}
func (releases) UploadAsset(context.Context, string, string, int64, string, io.Reader, int64, string) (*gitoperationslib.Asset, error) {
	return nil, notSupported("releases.upload_asset", noReleases)
}
func (releases) EditAsset(context.Context, string, string, int64, int64, gitoperationslib.EditAssetOptions) (*gitoperationslib.Asset, error) {
	return nil, notSupported("releases.edit_asset", noReleases)
}
func (releases) DeleteAsset(context.Context, string, string, int64, int64) error {
	return notSupported("releases.delete_asset", noReleases)
}
func (releases) DownloadAsset(context.Context, string, string, int64, int64) (io.ReadCloser, error) {
	return nil, notSupported("releases.download_asset", noReleases)
}

// TagProtections implements gitoperationslib.Client; every method returns
// ErrNotSupported.
func (c *Client) TagProtections() gitoperationslib.TagProtections { return tagProtections{} }

type tagProtections struct{}

func (tagProtections) List(context.Context, string, string) ([]gitoperationslib.TagProtection, error) {
	return nil, notSupported("tag_protections.list", noTagProtection)
}
func (tagProtections) Get(context.Context, string, string, string) (*gitoperationslib.TagProtection, error) {
	return nil, notSupported("tag_protections.get", noTagProtection)
}
func (tagProtections) Create(context.Context, string, string, gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	return nil, notSupported("tag_protections.create", noTagProtection)
}
func (tagProtections) Update(context.Context, string, string, string, gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	return nil, notSupported("tag_protections.update", noTagProtection)
}
func (tagProtections) Delete(context.Context, string, string, string) error {
	return notSupported("tag_protections.delete", noTagProtection)
}

// Issues implements gitoperationslib.Client; every method returns
// ErrNotSupported.
func (c *Client) Issues() gitoperationslib.Issues { return issues{} }

type issues struct{}

func (issues) List(context.Context, string, string, gitoperationslib.ListIssuesOptions) (gitoperationslib.Page[gitoperationslib.Issue], error) {
	return gitoperationslib.Page[gitoperationslib.Issue]{}, notSupported("issues.list", noIssues)
}
func (issues) Get(context.Context, string, string, int) (*gitoperationslib.Issue, error) {
	return nil, notSupported("issues.get", noIssues)
}
func (issues) Create(context.Context, string, string, gitoperationslib.CreateIssueOptions) (*gitoperationslib.Issue, error) {
	return nil, notSupported("issues.create", noIssues)
}
func (issues) Edit(context.Context, string, string, int, gitoperationslib.EditIssueOptions) (*gitoperationslib.Issue, error) {
	return nil, notSupported("issues.edit", noIssues)
}
func (issues) Delete(context.Context, string, string, int) error {
	return notSupported("issues.delete", noIssues)
}
func (issues) AddLabels(context.Context, string, string, int, []string) error {
	return notSupported("issues.add_labels", noIssues)
}
func (issues) RemoveLabel(context.Context, string, string, int, string) error {
	return notSupported("issues.remove_label", noIssues)
}
func (issues) ListComments(context.Context, string, string, int, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Comment], error) {
	return gitoperationslib.Page[gitoperationslib.Comment]{}, notSupported("issues.list_comments", noIssues)
}
func (issues) CreateComment(context.Context, string, string, int, string) (*gitoperationslib.Comment, error) {
	return nil, notSupported("issues.create_comment", noIssues)
}
func (issues) EditComment(context.Context, string, string, int64, string) (*gitoperationslib.Comment, error) {
	return nil, notSupported("issues.edit_comment", noIssues)
}
func (issues) DeleteComment(context.Context, string, string, int64) error {
	return notSupported("issues.delete_comment", noIssues)
}

// Labels implements gitoperationslib.Client; every method returns
// ErrNotSupported.
func (c *Client) Labels() gitoperationslib.Labels { return labels{} }

type labels struct{}

func (labels) List(context.Context, string, string, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
	return gitoperationslib.Page[gitoperationslib.Label]{}, notSupported("labels.list", noIssues)
}
func (labels) Get(context.Context, string, string, string) (*gitoperationslib.Label, error) {
	return nil, notSupported("labels.get", noIssues)
}
func (labels) Create(context.Context, string, string, gitoperationslib.Label) (*gitoperationslib.Label, error) {
	return nil, notSupported("labels.create", noIssues)
}
func (labels) Edit(context.Context, string, string, string, gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	return nil, notSupported("labels.edit", noIssues)
}
func (labels) Delete(context.Context, string, string, string) error {
	return notSupported("labels.delete", noIssues)
}

// Milestones implements gitoperationslib.Client; every method returns
// ErrNotSupported.
func (c *Client) Milestones() gitoperationslib.Milestones { return milestones{} }

type milestones struct{}

func (milestones) List(context.Context, string, string, gitoperationslib.MilestoneState, gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
	return gitoperationslib.Page[gitoperationslib.Milestone]{}, notSupported("milestones.list", noIssues)
}
func (milestones) Get(context.Context, string, string, int64) (*gitoperationslib.Milestone, error) {
	return nil, notSupported("milestones.get", noIssues)
}
func (milestones) Create(context.Context, string, string, gitoperationslib.CreateMilestoneOptions) (*gitoperationslib.Milestone, error) {
	return nil, notSupported("milestones.create", noIssues)
}
func (milestones) Edit(context.Context, string, string, int64, gitoperationslib.EditMilestoneOptions) (*gitoperationslib.Milestone, error) {
	return nil, notSupported("milestones.edit", noIssues)
}
func (milestones) Delete(context.Context, string, string, int64) error {
	return notSupported("milestones.delete", noIssues)
}
