package local

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAgainstGit compares the package with the git client on a real clone:
// GITOPSLIB_LOCAL_REPO=/path/to/clone go test -run AgainstGit -v ./local
// Read-only: it runs git's plumbing commands only.
func TestAgainstGit(t *testing.T) {
	dir := os.Getenv("GITOPSLIB_LOCAL_REPO")
	if dir == "" {
		t.Skip("GITOPSLIB_LOCAL_REPO not set")
	}
	needGit(t)
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	bs, err := r.Branches()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(git(t, dir, "for-each-ref", "--format=%(refname:short)=%(objectname)", "refs/heads"))
	var got []string
	for _, b := range bs {
		got = append(got, b.Name+"="+b.SHA)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("branches\n got %v\nwant %v", got, want)
	}
	def, _ := r.DefaultBranch("origin")
	if def == "" {
		def, _ = r.Current()
	}
	// More pairs, as "base...head" separated by commas, in GITOPSLIB_LOCAL_PAIRS.
	var pairs [][2]string
	for _, b := range bs {
		pairs = append(pairs, [2]string{def, b.Name})
	}
	for _, p := range strings.Split(os.Getenv("GITOPSLIB_LOCAL_PAIRS"), ",") {
		if base, head, ok := strings.Cut(p, "..."); ok {
			pairs = append(pairs, [2]string{base, head})
		}
	}
	for _, p := range pairs {
		base, head := p[0], p[1]
		start := time.Now()
		cmp, err := r.Compare(base, head)
		if err != nil {
			t.Errorf("%v: %v", p, err)
			continue
		}
		took := time.Since(start)
		w := git(t, dir, "rev-list", "--count", "--left-right", base+"..."+head)
		if g := fmt.Sprintf("%d\t%d", cmp.Behind, cmp.Ahead); g != w {
			t.Errorf("%v: behind, ahead %q, git %q", p, g, w)
		}
		mb, err := r.MergeBase(base, head)
		wmb := ""
		if out, e := gitTry(dir, "merge-base", base, head); e == nil {
			wmb = out
		}
		if err != nil || (mb == nil) != (wmb == "") || (mb != nil && mb.SHA != wmb) {
			t.Errorf("%v: merge base %v %v, git %q", p, mb, err, wmb)
		}
		o, _ := r.CreatedFrom(head)
		t.Logf("%s...%s: %d behind, %d ahead (%v), created from %+v", base, head, cmp.Behind, cmp.Ahead,
			took.Round(time.Millisecond), o)
	}
}
