package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strconv"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func toAsset(a *gh.ReleaseAsset) gitoperationslib.Asset {
	return gitoperationslib.Asset{
		ID:            a.GetID(),
		Name:          a.GetName(),
		Size:          int64(a.GetSize()),
		ContentType:   a.GetContentType(),
		DownloadCount: int64(a.GetDownloadCount()),
		DownloadURL:   a.GetBrowserDownloadURL(),
		CreatedAt:     a.GetCreatedAt().Time,
	}
}

func toRelease(r *gh.RepositoryRelease) gitoperationslib.Release {
	rel := gitoperationslib.Release{
		ID:              r.ID,
		TagName:         r.TagName,
		TargetCommitish: r.TargetCommitish,
		Name:            r.GetName(),
		Body:            r.GetBody(),
		Draft:           r.Draft,
		Prerelease:      r.Prerelease,
		Author:          r.GetAuthor().GetLogin(),
		HTMLURL:         r.HTMLURL,
		TarballURL:      r.GetTarballURL(),
		ZipballURL:      r.GetZipballURL(),
		CreatedAt:       r.CreatedAt.Time,
		PublishedAt:     r.GetPublishedAt().Time,
		Assets:          make([]gitoperationslib.Asset, len(r.Assets)),
	}
	for i, a := range r.Assets {
		rel.Assets[i] = toAsset(a)
	}
	return rel
}

// makeLatest converts the tri-state option to GitHub's string form.
func makeLatest(b *bool) *string {
	if b == nil {
		return nil
	}
	return gh.Ptr(strconv.FormatBool(*b))
}

// contentType returns ct, or a guess from name's extension.
func contentType(name, ct string) string {
	if ct != "" {
		return ct
	}
	if guess := mime.TypeByExtension(path.Ext(name)); guess != "" {
		return guess
	}
	return "application/octet-stream"
}

// Releases implements gitoperationslib.Client.
func (c *Client) Releases() gitoperationslib.Releases { return releases{c} }

type releases struct{ c *Client }

func (r releases) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Release], error) {
	list, resp, err := r.c.gh.Repositories.ListReleases(ctx, owner, repo, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Release]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Release]{Items: make([]gitoperationslib.Release, len(list)), NextPage: resp.NextPage}
	for i, rel := range list {
		page.Items[i] = toRelease(rel)
	}
	return page, nil
}

func single(rel *gh.RepositoryRelease, _ *gh.Response, err error) (*gitoperationslib.Release, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toRelease(rel)
	return &out, nil
}

func (r releases) Get(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.Release, error) {
	return single(r.c.gh.Repositories.GetRelease(ctx, owner, repo, id))
}

func (r releases) GetByTag(ctx context.Context, owner, repo, tag string) (*gitoperationslib.Release, error) {
	return single(r.c.gh.Repositories.GetReleaseByTag(ctx, owner, repo, tag))
}

func (r releases) Latest(ctx context.Context, owner, repo string) (*gitoperationslib.Release, error) {
	return single(r.c.gh.Repositories.GetLatestRelease(ctx, owner, repo))
}

func (r releases) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateReleaseOptions) (*gitoperationslib.Release, error) {
	if opts.TagName == "" {
		return nil, errors.New("github: releases.create: tag name is required")
	}
	if opts.HideArchiveLinks != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "releases.create", "hiding archive links")
	}
	req := gh.CreateReleaseRequest{
		TagName:         opts.TagName,
		TargetCommitish: nonEmpty(opts.TargetCommitish),
		Name:            nonEmpty(opts.Name),
		Body:            nonEmpty(opts.Body),
		Draft:           gh.Ptr(opts.Draft),
		Prerelease:      gh.Ptr(opts.Prerelease),
		MakeLatest:      makeLatest(opts.MakeLatest),
	}
	if opts.GenerateReleaseNotes {
		req.GenerateReleaseNotes = gh.Ptr(true)
	}
	return single(r.c.gh.Repositories.CreateRelease(ctx, owner, repo, req))
}

func (r releases) Edit(ctx context.Context, owner, repo string, id int64, opts gitoperationslib.EditReleaseOptions) (*gitoperationslib.Release, error) {
	if opts.HideArchiveLinks != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "releases.edit", "hiding archive links")
	}
	req := gh.UpdateReleaseRequest{
		TagName:         opts.TagName,
		TargetCommitish: opts.TargetCommitish,
		Name:            opts.Name,
		Body:            opts.Body,
		Draft:           opts.Draft,
		Prerelease:      opts.Prerelease,
		MakeLatest:      makeLatest(opts.MakeLatest),
	}
	return single(r.c.gh.Repositories.UpdateRelease(ctx, owner, repo, id, req))
}

func (r releases) Delete(ctx context.Context, owner, repo string, id int64) error {
	_, err := r.c.gh.Repositories.DeleteRelease(ctx, owner, repo, id)
	return convertError(err)
}

func (r releases) ListAssets(ctx context.Context, owner, repo string, releaseID int64, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Asset], error) {
	list, resp, err := r.c.gh.Repositories.ListReleaseAssets(ctx, owner, repo, releaseID, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Asset]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Asset]{Items: make([]gitoperationslib.Asset, len(list)), NextPage: resp.NextPage}
	for i, a := range list {
		page.Items[i] = toAsset(a)
	}
	return page, nil
}

func singleAsset(a *gh.ReleaseAsset, _ *gh.Response, err error) (*gitoperationslib.Asset, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toAsset(a)
	return &out, nil
}

// Asset IDs are global on GitHub; releaseID is not needed.
func (r releases) GetAsset(ctx context.Context, owner, repo string, _, assetID int64) (*gitoperationslib.Asset, error) {
	return singleAsset(r.c.gh.Repositories.GetReleaseAsset(ctx, owner, repo, assetID))
}

// UploadAsset streams content to the upload host. go-github's own helper
// only accepts an *os.File.
func (r releases) UploadAsset(ctx context.Context, owner, repo string, releaseID int64, name string, content io.Reader, size int64, ct string) (*gitoperationslib.Asset, error) {
	if name == "" {
		return nil, errors.New("github: releases.upload_asset: name is required")
	}
	if size < 0 {
		return nil, fmt.Errorf("github: releases.upload_asset: %q: size must be known", name)
	}
	u := fmt.Sprintf("repos/%s/%s/releases/%d/assets?name=%s",
		url.PathEscape(owner), url.PathEscape(repo), releaseID, url.QueryEscape(name))
	req, err := r.c.gh.NewUploadRequest(ctx, u, io.LimitReader(content, size), size, contentType(name, ct))
	if err != nil {
		return nil, fmt.Errorf("github: releases.upload_asset: %w", err)
	}
	var asset *gh.ReleaseAsset
	resp, err := r.c.gh.Do(req, &asset)
	return singleAsset(asset, resp, err)
}

func (r releases) EditAsset(ctx context.Context, owner, repo string, _, assetID int64, opts gitoperationslib.EditAssetOptions) (*gitoperationslib.Asset, error) {
	return singleAsset(r.c.gh.Repositories.UpdateReleaseAsset(ctx, owner, repo, assetID, gh.UpdateReleaseAssetRequest{Name: opts.Name}))
}

func (r releases) DeleteAsset(ctx context.Context, owner, repo string, _, assetID int64) error {
	_, err := r.c.gh.Repositories.DeleteReleaseAsset(ctx, owner, repo, assetID)
	return convertError(err)
}

func (r releases) DownloadAsset(ctx context.Context, owner, repo string, _, assetID int64) (io.ReadCloser, error) {
	rc, _, err := r.c.gh.Repositories.DownloadReleaseAsset(ctx, owner, repo, assetID, r.c.downloads)
	if err != nil {
		return nil, convertError(err)
	}
	return rc, nil
}
