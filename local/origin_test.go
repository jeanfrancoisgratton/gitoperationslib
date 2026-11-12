package local

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The user's case: branches created one from the other with no commits, so
// that only the reflogs can tell.
func TestCreatedFrom(t *testing.T) {
	needGit(t)
	dir := filepath.Join(t.TempDir(), "o")
	os.Mkdir(dir, 0o755)
	git(t, dir, "init", "-q", "-b", "BRANCH_1")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "a")
	git(t, dir, "switch", "-q", "-c", "BRANCH_1A")
	git(t, dir, "switch", "-q", "-c", "BRANCH_1A1")
	git(t, dir, "switch", "-q", "BRANCH_1")
	git(t, dir, "switch", "-q", "-c", "BRANCH_2")
	git(t, dir, "checkout", "-q", "-b", "BRANCH_2A")
	git(t, dir, "branch", "named", "BRANCH_1A")
	git(t, dir, "branch", "expr", "BRANCH_1A~0")
	git(t, dir, "switch", "-q", "-c", "renamed-src", "BRANCH_1A1")
	git(t, dir, "branch", "-m", "renamed-src", "renamed-dst")
	git(t, dir, "switch", "-q", "-c", "after-rename")
	git(t, dir, "switch", "-q", "--detach")
	git(t, dir, "switch", "-q", "-c", "from-detached")
	git(t, dir, "branch", "-m", "BRANCH_1A", "BRANCH_1A-new") // a parent renamed afterwards
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for branch, want := range map[string]Origin{
		"BRANCH_1A1":    {Branch: "BRANCH_1A-new", Exact: true},
		"BRANCH_2":      {Branch: "BRANCH_1", Exact: true},
		"BRANCH_2A":     {Branch: "BRANCH_2", Exact: true},
		"named":         {Branch: "BRANCH_1A-new", Exact: true},
		"expr":          {Branch: "BRANCH_1A-new"},
		"renamed-dst":   {Exact: true}, // created from BRANCH_1A1's commit, given as a branch: see below
		"after-rename":  {Branch: "renamed-dst", Exact: true},
		"from-detached": {Exact: true},
	} {
		o, err := r.CreatedFrom(branch)
		if err != nil || o == nil {
			t.Fatalf("%s: %v %v", branch, o, err)
		}
		if branch == "renamed-dst" {
			want.Branch = "BRANCH_1A1"
		}
		if o.Branch != want.Branch || o.Exact != want.Exact || o.At.IsZero() || o.SHA == "" {
			t.Errorf("%s: %+v, want %+v", branch, o, want)
		}
	}
	if o, _ := r.CreatedFrom("BRANCH_1"); o != nil {
		t.Errorf("the first branch has no starting point: %+v", o)
	}
}

func TestCreatedFromClone(t *testing.T) {
	needGit(t)
	src := history(t)
	dir := filepath.Join(t.TempDir(), "c")
	git(t, filepath.Dir(dir), "clone", "-q", src, dir)
	git(t, dir, "switch", "-q", "topic") // created from the remote branch
	r, _ := Open(dir)
	if o, err := r.CreatedFrom("main"); err != nil || o == nil || !o.Clone {
		t.Errorf("main: %+v %v", o, err)
	}
	if o, err := r.CreatedFrom("topic"); err != nil || o == nil || o.Branch != "origin/topic" || !o.Exact {
		t.Errorf("topic: %+v %v", o, err)
	}
}

func TestGuessParent(t *testing.T) {
	needGit(t)
	dir := filepath.Join(t.TempDir(), "g")
	os.Mkdir(dir, 0o755)
	commit := func(msg string) { git(t, dir, "commit", "-q", "--allow-empty", "-m", msg) }
	git(t, dir, "init", "-q", "-b", "main")
	commit("m1")
	git(t, dir, "switch", "-q", "-c", "a")
	commit("a1")
	time.Sleep(1100 * time.Millisecond) // commit dates order the guess
	git(t, dir, "switch", "-q", "-c", "a1")
	commit("a1-1")
	git(t, dir, "switch", "-q", "main")
	commit("m2")
	git(t, dir, "branch", "same") // at main's commit
	r, _ := Open(dir)
	defer r.Close()

	o, err := r.GuessParent("a1", nil)
	if err != nil || o == nil || o.Branch != "a" || o.Exact || len(o.Others) != 0 {
		t.Errorf("a1: %+v %v", o, err)
	}
	o, _ = r.GuessParent("a", nil) // a1 contains a: a child, not a parent
	if o == nil || o.Branch != "main" || !slices.Equal(o.Others, []string{"same"}) {
		t.Errorf("a: %+v", o)
	}
	o, _ = r.GuessParent("same", []string{"main"})
	if o == nil || o.Branch != "main" {
		t.Errorf("same: %+v", o)
	}
}
