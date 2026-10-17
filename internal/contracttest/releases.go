package contracttest

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Releases runs the release and asset checks in a throwaway repository.
func Releases(t *testing.T, env Env) {
	ctx := testContext(t)
	_, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	rels := env.Client.Releases()
	owner := env.Org
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// A draft for a new tag: assets are uploaded before publishing.
	var draft *gitoperationslib.Release
	check(retryConflict(ctx, t, func() (err error) {
		draft, err = rels.Create(ctx, owner, *repo, gitoperationslib.CreateReleaseOptions{
			TagName: "v1.0.1", Name: "1.0.1", Body: "Release notes", Draft: true,
		})
		return err
	}))
	if !draft.Draft || draft.TagName != "v1.0.1" || draft.Name != "1.0.1" || draft.Body != "Release notes" || !draft.PublishedAt.IsZero() {
		t.Errorf("draft = %+v", draft)
	}

	big := make([]byte, 1<<20+123)
	rand.Read(big)
	binary, err := rels.UploadAsset(ctx, owner, *repo, draft.ID, "tool_1.0.1_linux_amd64.tar.gz", bytes.NewReader(big), int64(len(big)), "")
	check(err)
	if binary.Name != "tool_1.0.1_linux_amd64.tar.gz" || binary.Size != int64(len(big)) {
		t.Errorf("binary = %+v", binary)
	}
	sums := []byte("deadbeef  tool_1.0.1_linux_amd64.tar.gz\n")
	checksums, err := rels.UploadAsset(ctx, owner, *repo, draft.ID, "SHA256SUMS", bytes.NewReader(sums), int64(len(sums)), "text/plain")
	check(err)

	assets, err := rels.ListAssets(ctx, owner, *repo, draft.ID, gitoperationslib.ListOptions{})
	check(err)
	if len(assets.Items) != 2 {
		t.Errorf("ListAssets = %+v", assets.Items)
	}
	got, err := rels.GetAsset(ctx, owner, *repo, draft.ID, binary.ID)
	check(err)
	if got.ID != binary.ID || got.Size != binary.Size {
		t.Errorf("GetAsset = %+v", got)
	}

	rc, err := rels.DownloadAsset(ctx, owner, *repo, draft.ID, binary.ID)
	check(err)
	downloaded, err := io.ReadAll(rc)
	rc.Close()
	check(err)
	if !bytes.Equal(downloaded, big) {
		t.Errorf("downloaded %d bytes, differ from the %d uploaded", len(downloaded), len(big))
	}

	renamed, err := rels.EditAsset(ctx, owner, *repo, draft.ID, checksums.ID, gitoperationslib.EditAssetOptions{Name: gitoperationslib.Ptr("SHA256SUMS.txt")})
	check(err)
	if renamed.Name != "SHA256SUMS.txt" {
		t.Errorf("renamed = %+v", renamed)
	}
	check(rels.DeleteAsset(ctx, owner, *repo, draft.ID, checksums.ID))
	assets, err = rels.ListAssets(ctx, owner, *repo, draft.ID, gitoperationslib.ListOptions{})
	check(err)
	if len(assets.Items) != 1 || assets.Items[0].ID != binary.ID {
		t.Errorf("after delete, ListAssets = %+v", assets.Items)
	}

	// Publish.
	published, err := rels.Edit(ctx, owner, *repo, draft.ID, gitoperationslib.EditReleaseOptions{Draft: gitoperationslib.Ptr(false)})
	check(err)
	if published.Draft || published.PublishedAt.IsZero() || len(published.Assets) != 1 {
		t.Errorf("published = %+v", published)
	}
	byTag, err := rels.GetByTag(ctx, owner, *repo, "v1.0.1")
	check(err)
	if byTag.ID != draft.ID {
		t.Errorf("GetByTag = %+v", byTag)
	}

	// A prerelease does not become the latest release.
	pre, err := rels.Create(ctx, owner, *repo, gitoperationslib.CreateReleaseOptions{TagName: "v1.1.0-rc1", Prerelease: true})
	check(err)
	latest, err := rels.Latest(ctx, owner, *repo)
	check(err)
	if latest.ID != published.ID {
		t.Errorf("Latest = %s, want v1.0.1", latest.TagName)
	}
	// GitHub's list endpoint lags slightly behind writes.
	var listed int
	if !eventually(ctx, func() bool {
		list, err := rels.List(ctx, owner, *repo, gitoperationslib.ListOptions{})
		check(err)
		listed = len(list.Items)
		return listed == 2
	}) {
		t.Errorf("List = %d releases, want 2", listed)
	}

	// Backend-specific options are either honoured or reported.
	switch env.Client.Backend() {
	case gitoperationslib.BackendForgejo:
		_, err := rels.Create(ctx, owner, *repo, gitoperationslib.CreateReleaseOptions{TagName: "v9", GenerateReleaseNotes: true})
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("GenerateReleaseNotes on Forgejo: err = %v", err)
		}
		hidden, err := rels.Edit(ctx, owner, *repo, pre.ID, gitoperationslib.EditReleaseOptions{HideArchiveLinks: gitoperationslib.Ptr(true)})
		check(err)
		_ = hidden
	case gitoperationslib.BackendGitHub:
		_, err := rels.Edit(ctx, owner, *repo, pre.ID, gitoperationslib.EditReleaseOptions{HideArchiveLinks: gitoperationslib.Ptr(true)})
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("HideArchiveLinks on GitHub: err = %v", err)
		}
		_, err = rels.Edit(ctx, owner, *repo, published.ID, gitoperationslib.EditReleaseOptions{MakeLatest: gitoperationslib.Ptr(true)})
		check(err)
	}

	if ext, ok := rels.(gitoperationslib.ExternalAssets); ok {
		link, err := ext.AddExternalAsset(ctx, owner, *repo, published.ID, "tool-mirror.tar.gz", "https://example.com/tool.tar.gz")
		check(err)
		if !link.External || link.DownloadURL != "https://example.com/tool.tar.gz" {
			t.Errorf("external asset = %+v", link)
		}
		if _, err := rels.DownloadAsset(ctx, owner, *repo, published.ID, link.ID); err == nil {
			t.Error("downloading an external asset should fail")
		}
	}

	check(rels.Delete(ctx, owner, *repo, pre.ID))
	if _, err := rels.Get(ctx, owner, *repo, pre.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}
