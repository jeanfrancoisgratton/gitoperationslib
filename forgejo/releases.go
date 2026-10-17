package forgejo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type attachmentJSON struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	DownloadCount int64     `json:"download_count"`
	DownloadURL   string    `json:"browser_download_url"`
	Type          string    `json:"type"` // "attachment" or "external"
	CreatedAt     time.Time `json:"created_at"`
}

func (a *attachmentJSON) toAsset() gitoperationslib.Asset {
	return gitoperationslib.Asset{
		ID:            a.ID,
		Name:          a.Name,
		Size:          a.Size,
		DownloadCount: a.DownloadCount,
		DownloadURL:   a.DownloadURL,
		External:      a.Type == "external",
		CreatedAt:     a.CreatedAt,
	}
}

type releaseJSON struct {
	ID              int64  `json:"id"`
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Name            string `json:"name"`
	Body            string `json:"body"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
	Author          *struct {
		Login string `json:"login"`
	} `json:"author"`
	HTMLURL     string           `json:"html_url"`
	TarballURL  string           `json:"tarball_url"`
	ZipballURL  string           `json:"zipball_url"`
	CreatedAt   time.Time        `json:"created_at"`
	PublishedAt time.Time        `json:"published_at"`
	Assets      []attachmentJSON `json:"assets"`
}

func (r *releaseJSON) toRelease() gitoperationslib.Release {
	rel := gitoperationslib.Release{
		ID:              r.ID,
		TagName:         r.TagName,
		TargetCommitish: r.TargetCommitish,
		Name:            r.Name,
		Body:            r.Body,
		Draft:           r.Draft,
		Prerelease:      r.Prerelease,
		HTMLURL:         r.HTMLURL,
		TarballURL:      r.TarballURL,
		ZipballURL:      r.ZipballURL,
		CreatedAt:       r.CreatedAt,
		PublishedAt:     r.PublishedAt,
		Assets:          make([]gitoperationslib.Asset, len(r.Assets)),
	}
	if r.Author != nil {
		rel.Author = r.Author.Login
	}
	if rel.Draft || rel.PublishedAt.Unix() <= 0 {
		rel.PublishedAt = time.Time{}
	}
	for i := range r.Assets {
		rel.Assets[i] = r.Assets[i].toAsset()
	}
	return rel
}

type releaseBody struct {
	TagName          *string `json:"tag_name,omitempty"`
	TargetCommitish  *string `json:"target_commitish,omitempty"`
	Name             *string `json:"name,omitempty"`
	Body             *string `json:"body,omitempty"`
	Draft            *bool   `json:"draft,omitempty"`
	Prerelease       *bool   `json:"prerelease,omitempty"`
	HideArchiveLinks *bool   `json:"hide_archive_links,omitempty"`
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

func releasesPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "releases"}, rest...)...)
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

// Releases implements gitoperationslib.Client.
func (c *Client) Releases() gitoperationslib.Releases { return releases{c} }

type releases struct{ c *Client }

var _ gitoperationslib.ExternalAssets = releases{}

func (r releases) get(ctx context.Context, p string) (*gitoperationslib.Release, error) {
	var out releaseJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, p, nil, nil, &out); err != nil {
		return nil, err
	}
	rel := out.toRelease()
	return &rel, nil
}

func (r releases) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Release], error) {
	var out []releaseJSON
	resp, err := r.c.api.Do(ctx, http.MethodGet, releasesPath(owner, repo), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Release]{}, err
	}
	return toPage(resp, out, (*releaseJSON).toRelease), nil
}

func (r releases) Get(ctx context.Context, owner, repo string, releaseID int64) (*gitoperationslib.Release, error) {
	return r.get(ctx, releasesPath(owner, repo, id(releaseID)))
}

func (r releases) GetByTag(ctx context.Context, owner, repo, tag string) (*gitoperationslib.Release, error) {
	return r.get(ctx, releasesPath(owner, repo, "tags", tag))
}

func (r releases) Latest(ctx context.Context, owner, repo string) (*gitoperationslib.Release, error) {
	return r.get(ctx, releasesPath(owner, repo, "latest"))
}

func (r releases) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateReleaseOptions) (*gitoperationslib.Release, error) {
	const op = "releases.create"
	if opts.TagName == "" {
		return nil, errors.New("forgejo: releases.create: tag name is required")
	}
	if opts.GenerateReleaseNotes {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "generated release notes")
	}
	if opts.MakeLatest != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "make latest")
	}
	body := releaseBody{
		TagName:          &opts.TagName,
		TargetCommitish:  nonEmpty(opts.TargetCommitish),
		Name:             &opts.Name,
		Body:             &opts.Body,
		Draft:            &opts.Draft,
		Prerelease:       &opts.Prerelease,
		HideArchiveLinks: opts.HideArchiveLinks,
	}
	var out releaseJSON
	if _, err := r.c.api.Do(ctx, http.MethodPost, releasesPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	rel := out.toRelease()
	return &rel, nil
}

func (r releases) Edit(ctx context.Context, owner, repo string, releaseID int64, opts gitoperationslib.EditReleaseOptions) (*gitoperationslib.Release, error) {
	if opts.MakeLatest != nil {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "releases.edit", "make latest")
	}
	body := releaseBody{
		TagName:          opts.TagName,
		TargetCommitish:  opts.TargetCommitish,
		Name:             opts.Name,
		Body:             opts.Body,
		Draft:            opts.Draft,
		Prerelease:       opts.Prerelease,
		HideArchiveLinks: opts.HideArchiveLinks,
	}
	var out releaseJSON
	if _, err := r.c.api.Do(ctx, http.MethodPatch, releasesPath(owner, repo, id(releaseID)), nil, body, &out); err != nil {
		return nil, err
	}
	rel := out.toRelease()
	return &rel, nil
}

func (r releases) Delete(ctx context.Context, owner, repo string, releaseID int64) error {
	_, err := r.c.api.Do(ctx, http.MethodDelete, releasesPath(owner, repo, id(releaseID)), nil, nil, nil)
	return err
}

func (r releases) ListAssets(ctx context.Context, owner, repo string, releaseID int64, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Asset], error) {
	var out []attachmentJSON
	resp, err := r.c.api.Do(ctx, http.MethodGet, releasesPath(owner, repo, id(releaseID), "assets"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Asset]{}, err
	}
	return toPage(resp, out, (*attachmentJSON).toAsset), nil
}

func (r releases) GetAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) (*gitoperationslib.Asset, error) {
	var out attachmentJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, releasesPath(owner, repo, id(releaseID), "assets", id(assetID)), nil, nil, &out); err != nil {
		return nil, err
	}
	asset := out.toAsset()
	return &asset, nil
}

func (r releases) UploadAsset(ctx context.Context, owner, repo string, releaseID int64, name string, content io.Reader, size int64, ct string) (*gitoperationslib.Asset, error) {
	if name == "" {
		return nil, errors.New("forgejo: releases.upload_asset: name is required")
	}
	file := &forgejoapi.FormFile{
		Field: "attachment", Filename: name, ContentType: contentType(name, ct), Content: content, Size: size,
	}
	var out attachmentJSON
	if _, err := r.c.api.Upload(ctx, releasesPath(owner, repo, id(releaseID), "assets"), url.Values{"name": {name}}, nil, file, &out); err != nil {
		return nil, err
	}
	asset := out.toAsset()
	return &asset, nil
}

func (r releases) AddExternalAsset(ctx context.Context, owner, repo string, releaseID int64, name, link string) (*gitoperationslib.Asset, error) {
	if name == "" || link == "" {
		return nil, errors.New("forgejo: releases.add_external_asset: name and url are required")
	}
	var out attachmentJSON
	if _, err := r.c.api.Upload(ctx, releasesPath(owner, repo, id(releaseID), "assets"), url.Values{"name": {name}},
		map[string]string{"external_url": link}, nil, &out); err != nil {
		return nil, err
	}
	asset := out.toAsset()
	return &asset, nil
}

func (r releases) EditAsset(ctx context.Context, owner, repo string, releaseID, assetID int64, opts gitoperationslib.EditAssetOptions) (*gitoperationslib.Asset, error) {
	body := struct {
		Name *string `json:"name,omitempty"`
	}{opts.Name}
	var out attachmentJSON
	if _, err := r.c.api.Do(ctx, http.MethodPatch, releasesPath(owner, repo, id(releaseID), "assets", id(assetID)), nil, body, &out); err != nil {
		return nil, err
	}
	asset := out.toAsset()
	return &asset, nil
}

func (r releases) DeleteAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) error {
	_, err := r.c.api.Do(ctx, http.MethodDelete, releasesPath(owner, repo, id(releaseID), "assets", id(assetID)), nil, nil, nil)
	return err
}

func (r releases) DownloadAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) (io.ReadCloser, error) {
	asset, err := r.GetAsset(ctx, owner, repo, releaseID, assetID)
	if err != nil {
		return nil, err
	}
	if asset.External {
		return nil, fmt.Errorf("forgejo: releases.download_asset: %q is an external link (%s); fetch it directly", asset.Name, asset.DownloadURL)
	}
	return r.c.api.Download(ctx, asset.DownloadURL)
}

// nonEmpty returns nil for "", so the field is omitted from the request.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
