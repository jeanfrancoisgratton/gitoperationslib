package local

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// history builds a repository with branches, merges and long, similar commit
// messages, so that a repack stores commits as deltas.
func history(t *testing.T) string {
	needGit(t)
	dir := filepath.Join(t.TempDir(), "h")
	os.Mkdir(dir, 0o755)
	git(t, dir, "init", "-q", "-b", "main")
	body := strings.Repeat("a line of the message that repeats, to give deltas something to work on\n", 40)
	commit := func(msg string) {
		file, _, _ := strings.Cut(msg, " ") // one file per branch: the merge has no conflict
		os.WriteFile(filepath.Join(dir, file), []byte(msg+body), 0o644)
		git(t, dir, "add", file)
		git(t, dir, "commit", "-q", "-m", msg+"\n\n"+body)
	}
	for i := range 30 {
		commit("main " + strconv.Itoa(i))
	}
	git(t, dir, "switch", "-q", "-c", "topic", "main~10")
	for i := range 12 {
		commit("topic " + strconv.Itoa(i))
	}
	git(t, dir, "switch", "-q", "main")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge topic", "topic~4")
	commit("main after merge")
	git(t, dir, "switch", "-q", "--orphan", "unrelated")
	commit("unrelated")
	git(t, dir, "switch", "-q", "main")
	git(t, dir, "tag", "-a", "-m", "annotated", "v1", "main~3")
	return dir
}

func checkCompare(t *testing.T, dir string) {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, pair := range [][2]string{
		{"main", "topic"}, {"topic", "main"}, {"main", "main"}, {"v1", "topic"}, {"main", "unrelated"},
		{"topic~4", "main"},
	} {
		base, head := pair[0], pair[1]
		if strings.Contains(base, "~") { // Resolve takes refs, not expressions
			base = git(t, dir, "rev-parse", base)
		}
		cmp, err := r.Compare(base, head)
		if err != nil {
			t.Fatalf("%v: %v", pair, err)
		}
		want := git(t, dir, "rev-list", "--count", "--left-right", base+"..."+head)
		if got := fmt.Sprintf("%d\t%d", cmp.Behind, cmp.Ahead); got != want {
			t.Errorf("%v: behind, ahead %q, want %q", pair, got, want)
		}
	}
	mb, err := r.MergeBase("main", "topic")
	if err != nil || mb.SHA != git(t, dir, "merge-base", "main", "topic") {
		t.Errorf("merge base %v %v", mb, err)
	}
	if mb, err := r.MergeBase("main", "unrelated"); mb != nil || err != nil {
		t.Errorf("unrelated: %v %v", mb, err)
	}
	if ok, _ := r.IsAncestor(git(t, dir, "rev-parse", "topic~4"), "main"); !ok {
		t.Error("topic~4 should be merged into main")
	}
	if ok, _ := r.IsAncestor("topic", "main"); ok {
		t.Error("topic is not merged into main")
	}
	c, err := r.Commit("v1")
	if err != nil || c.SHA != git(t, dir, "rev-parse", "v1^{commit}") || !strings.HasPrefix(c.Message, "main 28\n") ||
		c.Author != "t" || c.AuthorEmail != "t@example.com" || c.Date.IsZero() || len(c.Parents) != 1 {
		t.Errorf("commit %+v %v", c, err)
	}
}

func TestCompareLoose(t *testing.T) {
	checkCompare(t, history(t))
}

func TestComparePacked(t *testing.T) {
	dir := history(t)
	git(t, dir, "repack", "-q", "-a", "-d", "-f", "--depth=50", "--window=250")
	checkCompare(t, dir)
}

// Every object, packed with deltas, reads back with its own SHA.
func TestReadAllObjects(t *testing.T) {
	dir := history(t)
	git(t, dir, "repack", "-q", "-a", "-d", "-f", "--depth=50", "--window=250")
	// Plus loose objects.
	os.WriteFile(filepath.Join(dir, "main"), []byte("new"), 0o644)
	git(t, dir, "commit", "-q", "-am", "loose one")
	if n := git(t, dir, "count-objects", "-v"); !strings.Contains(n, "packs: 1") {
		t.Fatalf("expected one pack: %s", n)
	}
	r, _ := Open(dir)
	defer r.Close()
	s, err := r.objects()
	if err != nil {
		t.Fatal(err)
	}
	names := map[int]string{objCommit: "commit", objTree: "tree", objBlob: "blob", objTag: "tag"}
	all := strings.Fields(git(t, dir, "cat-file", "--batch-all-objects", "--batch-check=%(objectname)"))
	if len(all) < 100 {
		t.Fatalf("only %d objects", len(all))
	}
	for _, sha := range all {
		o, err := s.read(sha)
		if err != nil {
			t.Fatalf("%s: %v", sha, err)
		}
		h := sha1.Sum(append([]byte(fmt.Sprintf("%s %d\x00", names[o.typ], len(o.data))), o.data...))
		if hex.EncodeToString(h[:]) != sha {
			t.Fatalf("%s reads back as %s", sha, hex.EncodeToString(h[:]))
		}
	}
	if _, err := s.read(strings.Repeat("0", 40)); err == nil {
		t.Error("a missing object should fail")
	}
}

func TestShallow(t *testing.T) {
	src := history(t)
	dir := filepath.Join(t.TempDir(), "s")
	git(t, filepath.Dir(dir), "clone", "-q", "--depth=3", "file://"+src, dir)
	r, _ := Open(dir)
	defer r.Close()
	cmp, err := r.Compare("main", "main")
	if err != nil || cmp.Ahead != 0 {
		t.Fatal(cmp, err)
	}
	// main~2 is the boundary: no parents, and walking does not fail.
	tip := git(t, dir, "rev-parse", "main~2")
	c, err := r.Commit(tip)
	if err != nil || len(c.Parents) != 0 {
		t.Errorf("%+v %v", c, err)
	}
	if _, err := r.Compare(tip, "main"); err != nil {
		t.Error(err)
	}
}

func TestResolveOrder(t *testing.T) {
	dir := history(t)
	git(t, dir, "tag", "topic", "main") // a tag named like a branch
	r, _ := Open(dir)
	defer r.Close()
	for ref, rev := range map[string]string{
		"topic": "refs/tags/topic", "refs/heads/topic": "refs/heads/topic", "HEAD": "HEAD", "v1": "v1^{commit}",
	} {
		got, err := r.Resolve(ref)
		if want := git(t, dir, "rev-parse", rev); err != nil || got != want {
			t.Errorf("%s: %s %v, want %s", ref, got, err, want)
		}
	}
	if _, err := r.Resolve("nope"); err == nil {
		t.Error("a missing ref resolved")
	}
}
