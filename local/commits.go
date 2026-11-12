package local

import (
	"bytes"
	"container/heap"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
)

// Commit is a commit read from the repository.
type Commit struct {
	SHA     string   `json:"sha"`
	Parents []string `json:"parents,omitzero"` // none for a root commit, or a shallow clone's boundary
	// Message is the full message; its first line is the title.
	Message       string    `json:"message"`
	Author        string    `json:"author"` // the author's name
	AuthorEmail   string    `json:"authorEmail,omitzero"`
	Date          time.Time `json:"date"`          // the author date
	CommitterDate time.Time `json:"committerDate"` // when it was last rewritten, as by a rebase
}

// Summary is the commit as the forges' branch lists describe it.
func (c *Commit) Summary() *gol.CommitSummary {
	return &gol.CommitSummary{Message: c.Message, Author: c.Author, Date: c.Date}
}

// Resolve turns a ref into a commit SHA: a full SHA, a tag (annotated ones are
// peeled), a local branch, a "remote/branch", a full "refs/..." name, or HEAD.
func (r *Repo) Resolve(ref string) (string, error) {
	if len(ref) == 40 && strings.Trim(strings.ToLower(ref), "0123456789abcdef") == "" {
		return strings.ToLower(ref), nil
	}
	if ref == "HEAD" {
		b, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD"))
		if err != nil {
			return "", err
		}
		h := strings.TrimSpace(string(b))
		name, ok := strings.CutPrefix(h, "ref: refs/heads/")
		if !ok {
			return h, nil
		}
		ref = name
	}
	// git's order: a tag wins over a branch of the same name.
	for _, prefix := range []string{"refs/tags/", "refs/heads/", "refs/remotes/"} {
		name := ref
		if strings.HasPrefix(ref, "refs/") {
			var ok bool
			if name, ok = strings.CutPrefix(ref, prefix); !ok {
				continue
			}
		}
		refs, err := r.refs(prefix)
		if err != nil {
			return "", err
		}
		if sha, ok := refs[name]; ok {
			if prefix == "refs/tags/" {
				return r.peel(sha)
			}
			return sha, nil
		}
	}
	return "", fmt.Errorf("%w: no branch, remote branch or tag %q", ErrObjectNotFound, ref)
}

// peel follows annotated tags to the commit.
func (r *Repo) peel(sha string) (string, error) {
	s, err := r.objects()
	if err != nil {
		return "", err
	}
	for range 10 {
		o, err := s.read(sha)
		if err != nil {
			return "", err
		}
		if o.typ != objTag {
			return sha, nil
		}
		target, _, _ := bytes.Cut(o.data, []byte{'\n'})
		sha = string(bytes.TrimPrefix(target, []byte("object ")))
	}
	return "", errors.New("tag chain too long")
}

// Commit reads a commit; ref is as for Resolve.
func (r *Repo) Commit(ref string) (*Commit, error) {
	sha, err := r.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return r.commit(sha)
}

func (r *Repo) commit(sha string) (*Commit, error) {
	s, err := r.objects()
	if err != nil {
		return nil, err
	}
	o, err := s.read(sha)
	if err != nil {
		return nil, err
	}
	if o.typ != objCommit {
		return nil, fmt.Errorf("%s is not a commit", sha)
	}
	c := &Commit{SHA: sha}
	headers, msg, _ := bytes.Cut(o.data, []byte("\n\n"))
	c.Message = string(msg)
	for line := range strings.Lines(string(headers)) {
		line = strings.TrimSuffix(line, "\n")
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "parent":
			c.Parents = append(c.Parents, val)
		case "author":
			c.Author, c.AuthorEmail, c.Date = parseIdent(val)
		case "committer":
			_, _, c.CommitterDate = parseIdent(val)
		}
	}
	if r.isShallow(sha) {
		c.Parents = nil // the clone does not have them
	}
	return c, nil
}

// parseIdent splits "Name <email> 1700000000 -0500".
func parseIdent(v string) (name, email string, when time.Time) {
	name, rest, _ := strings.Cut(v, " <")
	email, rest, _ = strings.Cut(rest, "> ")
	ts, tz, _ := strings.Cut(rest, " ")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return name, email, time.Time{}
	}
	when = time.Unix(sec, 0)
	if len(tz) == 5 {
		h, _ := strconv.Atoi(tz[1:3])
		m, _ := strconv.Atoi(tz[3:5])
		off := h*3600 + m*60
		if tz[0] == '-' {
			off = -off
		}
		when = when.In(time.FixedZone(tz, off))
	}
	return name, email, when
}

func (r *Repo) isShallow(sha string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shallow == nil {
		r.shallow = map[string]bool{}
		if b, err := os.ReadFile(filepath.Join(r.commonDir, "shallow")); err == nil {
			for _, l := range strings.Fields(string(b)) {
				r.shallow[l] = true
			}
		}
	}
	return r.shallow[sha]
}

// The two sides of a walk.
const (
	sideBase = 1 << iota
	sideHead
	sideBoth = sideBase | sideHead
)

type walkItem struct {
	c     *Commit
	sides int
}

type walkQueue []*walkItem

func (q walkQueue) Len() int           { return len(q) }
func (q walkQueue) Less(i, j int) bool { return q[i].c.CommitterDate.After(q[j].c.CommitterDate) }
func (q walkQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *walkQueue) Push(x any)        { *q = append(*q, x.(*walkItem)) }
func (q *walkQueue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// walk paints the commits reachable from base and head, newest first, and stops
// once every commit left to look at is reachable from both. It returns each
// commit seen with the sides that reach it, and the newest common commit.
// Like git without a commit-graph, a commit dated before its parents (a skewed
// clock) can make the counts slightly off.
func (r *Repo) walk(base, head string) (map[string]int, *Commit, error) {
	seen := map[string]int{}
	byID := map[string]*walkItem{}
	q := &walkQueue{}
	queued := map[string]int{} // entries in the queue, by commit
	open := 0                  // entries in the queue not yet reached from both sides
	push := func(it *walkItem) {
		queued[it.c.SHA]++
		if it.sides != sideBoth {
			open++
		}
		heap.Push(q, &walkItem{c: it.c, sides: it.sides})
	}
	add := func(sha string, sides int) error {
		if it, ok := byID[sha]; ok {
			if it.sides|sides == it.sides {
				return nil
			}
			if it.sides|sides == sideBoth {
				open -= queued[sha] // its queued entries are now common ones
			}
			it.sides |= sides
			seen[sha] = it.sides
			push(it) // spread the new side further
			return nil
		}
		c, err := r.commit(sha)
		if err != nil {
			return err
		}
		it := &walkItem{c: c, sides: sides}
		byID[sha], seen[sha] = it, sides
		push(it)
		return nil
	}
	if err := add(base, sideBase); err != nil {
		return nil, nil, err
	}
	if err := add(head, sideHead); err != nil {
		return nil, nil, err
	}
	// Once only common commits are left, keep going while they are not older
	// than the oldest one-sided commit: one of them may still lead to it, which
	// makes it common. Commits made in the same second (scripts, rebases) need
	// this.
	var oldest time.Time
	measured := false
	for q.Len() > 0 {
		if open == 0 {
			// Only common commits get queued from now on: no new one-sided ones.
			for sha, sides := range seen {
				if measured {
					break
				}
				if d := byID[sha].c.CommitterDate; sides != sideBoth && (oldest.IsZero() || d.Before(oldest)) {
					oldest = d
				}
			}
			measured = true
			if oldest.IsZero() || (*q)[0].c.CommitterDate.Before(oldest) {
				break
			}
		}
		it := heap.Pop(q).(*walkItem)
		queued[it.c.SHA]--
		sides := byID[it.c.SHA].sides
		if sides != sideBoth {
			open--
		}
		for _, p := range it.c.Parents {
			if err := add(p, sides); err != nil {
				return nil, nil, err
			}
		}
	}
	// The merge bases are the common commits with a one-sided child, less those
	// reachable from another of them (main's fork point, once part of the branch
	// was merged back). The newest is returned.
	candidates := map[string]bool{}
	for sha, sides := range seen {
		if sides == sideBoth {
			continue
		}
		for _, p := range byID[sha].c.Parents {
			if seen[p] == sideBoth {
				candidates[p] = true
			}
		}
	}
	var stack []string
	for sha := range candidates {
		stack = append(stack, byID[sha].c.Parents...)
	}
	for reached := map[string]bool{}; len(stack) > 0; {
		sha := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		it, walked := byID[sha]
		if reached[sha] || !walked {
			continue
		}
		reached[sha] = true
		delete(candidates, sha)
		stack = append(stack, it.c.Parents...)
	}
	var mergeBase *Commit
	for sha := range candidates {
		c := byID[sha].c
		if mergeBase == nil || c.CommitterDate.After(mergeBase.CommitterDate) ||
			(c.CommitterDate.Equal(mergeBase.CommitterDate) && c.SHA < mergeBase.SHA) {
			mergeBase = c
		}
	}
	return seen, mergeBase, nil
}

// Compare counts the commits between base and head, as the forges' Compare
// does; refs are as for Resolve.
func (r *Repo) Compare(base, head string) (*gol.Comparison, error) {
	b, err := r.Resolve(base)
	if err != nil {
		return nil, err
	}
	h, err := r.Resolve(head)
	if err != nil {
		return nil, err
	}
	out := &gol.Comparison{Base: base, Head: head}
	if b == h {
		return out, nil
	}
	seen, _, err := r.walk(b, h)
	if err != nil {
		return nil, err
	}
	for _, sides := range seen {
		switch sides {
		case sideHead:
			out.Ahead++
		case sideBase:
			out.Behind++
		}
	}
	return out, nil
}

// IsAncestor tells if ancestor is reachable from descendant (a ref is its own
// ancestor): for a branch, whether it is merged into the other.
func (r *Repo) IsAncestor(ancestor, descendant string) (bool, error) {
	cmp, err := r.Compare(descendant, ancestor)
	if err != nil {
		return false, err
	}
	return cmp.Ahead == 0, nil
}

// MergeBase is the newest commit reachable from both refs; nil when their
// histories are unrelated.
func (r *Repo) MergeBase(a, b string) (*Commit, error) {
	sa, err := r.Resolve(a)
	if err != nil {
		return nil, err
	}
	sb, err := r.Resolve(b)
	if err != nil {
		return nil, err
	}
	if sa == sb {
		return r.commit(sa)
	}
	_, mb, err := r.walk(sa, sb)
	return mb, err
}
