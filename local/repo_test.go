package local

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs the git client in dir, for building test clones only: the package
// itself never runs it.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the git client is needed to build test clones")
	}
}

// fixture builds an upstream repository and a clone of it, with branches,
// an upstream that is gone, a local upstream and a linked worktree.
func fixture(t *testing.T) (clone, worktree string) {
	needGit(t)
	base := t.TempDir()
	up := filepath.Join(base, "up")
	os.Mkdir(up, 0o755)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "first")
	git(t, up, "branch", "gone")
	clone = filepath.Join(base, "clone")
	git(t, base, "clone", "-q", up, clone)
	git(t, clone, "remote", "add", "github", "git@github.com:acme/widget.git")
	git(t, clone, "switch", "-q", "-c", "feature")
	git(t, clone, "commit", "-q", "--allow-empty", "-m", "f")
	git(t, clone, "switch", "-q", "-c", "gone", "--track", "origin/gone")
	git(t, clone, "branch", "-q", "--track", "child", "feature")
	git(t, clone, "switch", "-q", "main")
	git(t, up, "branch", "-D", "gone")
	git(t, clone, "fetch", "-q", "--prune")
	worktree = filepath.Join(base, "wt")
	git(t, clone, "worktree", "add", "-q", worktree, "feature")
	git(t, clone, "pack-refs", "--all") // some refs packed, then one loose again
	git(t, clone, "branch", "loose")
	return clone, worktree
}

func TestBranches(t *testing.T) {
	clone, wt := fixture(t)
	r, err := Open(filepath.Join(clone, "sub", "..")) // a path inside the working copy
	if err != nil {
		t.Fatal(err)
	}
	bs, err := r.Branches()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Branch{}
	var names []string
	for _, b := range bs {
		by[b.Name] = b
		names = append(names, b.Name)
	}
	if got := strings.Join(names, ","); got != "child,feature,gone,loose,main" {
		t.Fatalf("branches %s", got)
	}
	for _, b := range bs {
		if want := git(t, clone, "rev-parse", "refs/heads/"+b.Name); b.SHA != want {
			t.Errorf("%s: SHA %s, want %s", b.Name, b.SHA, want)
		}
	}
	main := by["main"]
	if !main.Current || main.Worktree != clone || main.Upstream == nil ||
		main.Upstream.Remote != "origin" || main.Upstream.Branch != "main" || main.Upstream.SHA != main.SHA {
		t.Errorf("main: %+v %+v", main, main.Upstream)
	}
	if f := by["feature"]; f.Current || f.Worktree != wt || f.Upstream != nil {
		t.Errorf("feature: %+v", f)
	}
	if g := by["gone"]; g.Upstream == nil || g.Upstream.Branch != "gone" || g.Upstream.SHA != "" {
		t.Errorf("gone: %+v", g.Upstream)
	}
	if c := by["child"]; c.Upstream == nil || c.Upstream.Remote != "." || c.Upstream.SHA != by["feature"].SHA {
		t.Errorf("child: %+v", c.Upstream)
	}

	// From the linked worktree: same branches, another current one.
	w, err := Open(wt)
	if err != nil {
		t.Fatal(err)
	}
	if cur, _ := w.Current(); cur != "feature" {
		t.Errorf("worktree current %q", cur)
	}
	if def, _ := w.DefaultBranch("origin"); def != "main" {
		t.Errorf("default %q", def)
	}
	rb, _ := w.RemoteBranches()
	if _, ok := rb["origin/gone"]; ok || rb["origin/main"] == "" {
		t.Errorf("remote branches %v", rb)
	}
}

func TestRemotes(t *testing.T) {
	clone, _ := fixture(t)
	r, _ := Open(clone)
	rs, err := r.Remotes()
	if err != nil || len(rs) != 2 || rs[0].Name != "github" || rs[1].Name != "origin" {
		t.Fatalf("%v %+v", err, rs)
	}
	got, err := r.RepoOn("https://api.github.com")
	if err != nil || got.Owner != "acme" || got.Name != "widget" || got.Remote != "github" {
		t.Errorf("%v %+v", err, got)
	}
	if _, err := r.RepoOn("git.example.com"); !errors.Is(err, ErrNoRemoteOnHost) {
		t.Errorf("other host: %v", err)
	}
	if ok, _ := r.MatchesRepo("", "WIDGET"); !ok {
		t.Error("MatchesRepo")
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNoWorkingCopy) {
		t.Errorf("%v", err)
	}
	needGit(t)
	bare := filepath.Join(t.TempDir(), "b.git")
	git(t, filepath.Dir(bare), "init", "-q", "--bare", "-b", "main", bare)
	r, err := Open(bare)
	if err != nil {
		t.Fatal(err)
	}
	if bs, err := r.Branches(); err != nil || len(bs) != 0 {
		t.Errorf("%v %v", err, bs)
	}
}

func TestParseRemoteURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://git.example.com/acme/widget.git":    "git.example.com acme widget",
		"ssh://git@git.example.com:2222/acme/widget": "git.example.com acme widget",
		"git@ssh.bitbucket.org:acme/widget.git":      "ssh.bitbucket.org acme widget",
		"/srv/git/widget.git":                        "",
		"file:///srv/acme/widget":                    "",
	} {
		h, o, n, ok := ParseRemoteURL(in)
		got := ""
		if ok {
			got = h + " " + o + " " + n
		}
		if got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

func TestConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte(`[core]
	bare
[remote "my.remote"]
	url = "https://x/a/b" ; comment
[Branch "Feat/X"] remote = .
	merge = refs/heads/main # comment
[remote.Old]
	url = u
`), 0o644)
	c, err := readConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"core.bare": "true", "remote.my.remote.url": "https://x/a/b",
		"branch.Feat/X.remote": ".", "branch.Feat/X.merge": "refs/heads/main", "remote.old.url": "u",
	} {
		if got := c.get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// gitTry runs git and returns its error instead of failing.
func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
