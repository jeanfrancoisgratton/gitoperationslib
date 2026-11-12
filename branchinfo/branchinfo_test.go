package branchinfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib/local"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestCollectLocal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the git client is needed to build test clones")
	}
	base := t.TempDir()
	up, dir := filepath.Join(base, "up"), filepath.Join(base, "clone")
	os.Mkdir(up, 0o755)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "first")
	git(t, base, "clone", "-q", up, dir)
	git(t, dir, "switch", "-q", "-c", "feature")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "f1")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "f2")
	git(t, dir, "switch", "-q", "-c", "sub")
	git(t, dir, "switch", "-q", "main")
	git(t, dir, "branch", "merged")

	repo, err := local.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	res, err := Collect(context.Background(), Options{Local: repo, AheadBehind: Local, LastCommit: Local, Parent: Local})
	if err != nil || res.Incomplete != nil {
		t.Fatal(err, res.Incomplete)
	}
	if res.Default != "main" || len(res.Branches) != 4 {
		t.Fatalf("%+v", res)
	}
	by := map[string]Branch{}
	for _, b := range res.Branches {
		by[b.Name] = b
	}
	if b := by["feature"]; b.LocalAhead == nil || b.LocalAhead.Ahead != 2 || *b.LocalMerged || b.Origin == nil ||
		b.Origin.Branch != "main" || !b.Origin.Exact || b.LocalCommit == nil || b.LocalCommit.Message != "f2\n" {
		t.Errorf("feature: %+v %+v %+v", b, b.LocalAhead, b.Origin)
	}
	if b := by["sub"]; b.Origin == nil || b.Origin.Branch != "feature" {
		t.Errorf("sub: %+v", b.Origin)
	}
	if b := by["merged"]; !*b.LocalMerged || b.Default || b.Server != nil {
		t.Errorf("merged: %+v", b)
	}
	if !by["main"].Default {
		t.Error("main is the default")
	}
	if _, err := Collect(context.Background(), Options{}); err == nil {
		t.Error("no source accepted")
	}
}
