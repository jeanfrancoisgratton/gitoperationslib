package local

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Origin is where a branch was created from.
type Origin struct {
	// Branch is the branch it was created from: "main", or "origin/main" for a
	// remote one. Empty when it was created from a commit, or by a clone.
	Branch string `json:"branch,omitzero"`
	// SHA is the commit it started at (CreatedFrom), or where it forked from
	// Branch (GuessParent).
	SHA string `json:"sha,omitzero"`
	// At is when it was created; zero for a guess.
	At time.Time `json:"at,omitzero"`
	// Clone is set for the branch git clone made: its remote's default branch.
	Clone bool `json:"clone,omitzero"`
	// Exact is set when the reflogs name the branch. Guesses from the history,
	// and creations from an expression such as "main~2", are not exact.
	Exact bool `json:"exact,omitzero"`
	// Others are branches as likely as Branch, for a guess: branches at the same
	// commit cannot be told apart from the history.
	Others []string `json:"others,omitzero"`
}

// reflogEntry is one line of a reflog.
type reflogEntry struct {
	old, new string
	at       time.Time
	msg      string
}

// readReflog reads a reflog, oldest first; a missing one is empty.
func readReflog(path string) ([]reflogEntry, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []reflogEntry
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		head, msg, _ := strings.Cut(s.Text(), "\t")
		fields := strings.Fields(head)
		if len(fields) < 4 {
			continue
		}
		sec, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, reflogEntry{old: fields[0], new: fields[1], at: time.Unix(sec, 0), msg: msg})
	}
	return out, s.Err()
}

func isSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

// headLogs are the reflogs of HEAD: the main working copy's and each linked
// worktree's.
func (r *Repo) headLogs() ([][]reflogEntry, error) {
	paths := []string{filepath.Join(r.commonDir, "logs", "HEAD")}
	more, _ := filepath.Glob(filepath.Join(r.commonDir, "worktrees", "*", "logs", "HEAD"))
	var out [][]reflogEntry
	for _, p := range append(paths, more...) {
		l, err := readReflog(p)
		if err != nil {
			return nil, err
		}
		if len(l) > 0 {
			out = append(out, l)
		}
	}
	return out, nil
}

// renames maps each old branch name to its new one, from every branch's reflog.
func (r *Repo) renames() (map[string]string, error) {
	out := map[string]string{}
	root := filepath.Join(r.commonDir, "logs", "refs", "heads")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		l, err := readReflog(path)
		for _, e := range l {
			if from, to, ok := parseRename(e.msg); ok {
				out[from] = to
			}
		}
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// parseRename reads "Branch: renamed refs/heads/a to refs/heads/b".
func parseRename(msg string) (from, to string, ok bool) {
	rest, ok := strings.CutPrefix(msg, "Branch: renamed refs/heads/")
	if !ok {
		return "", "", false
	}
	from, to, ok = strings.Cut(rest, " to refs/heads/")
	return from, to, ok
}

// forward follows renames to a branch's current name.
func forward(name string, renames map[string]string) string {
	for range len(renames) + 1 {
		next, ok := renames[name]
		if !ok {
			break
		}
		name = next
	}
	return name
}

// CreatedFrom reads where a branch was created from, in the reflogs. It
// returns nil when they do not say: the branch was created before the reflog's
// expiry (90 days by default), in another clone, or by a command that logs no
// starting point.
//
// "git switch -c" and "git checkout -b" log "Created from HEAD"; the branch
// HEAD was on is then read from HEAD's reflog, with renames since followed.
func (r *Repo) CreatedFrom(branch string) (*Origin, error) {
	own, err := readReflog(filepath.Join(r.commonDir, "logs", "refs", "heads", filepath.FromSlash(branch)))
	if err != nil || len(own) == 0 {
		return nil, err
	}
	first := own[0]
	o := &Origin{SHA: first.new, At: first.at}
	if _, ok := strings.CutPrefix(first.msg, "clone: from "); ok {
		o.Clone, o.Exact = true, true
		return o, nil
	}
	from, ok := strings.CutPrefix(first.msg, "branch: Created from ")
	if !ok {
		return nil, nil
	}
	renames, err := r.renames()
	if err != nil {
		return nil, err
	}
	expr := strings.IndexAny(from, "~^@:") >= 0
	start := from
	if expr {
		start = from[:strings.IndexAny(from, "~^@:")]
	}
	switch {
	case start == "HEAD":
		if err := r.fromHEAD(o, branch, own, renames); err != nil {
			return nil, err
		}
		if expr {
			o.Exact = false
		}
	case isSHA(start):
		o.Exact = !expr
	default:
		name, remote := strings.CutPrefix(start, "refs/remotes/")
		if !remote {
			name = strings.TrimPrefix(start, "refs/heads/")
			name = forward(name, renames)
		}
		o.Branch, o.Exact = name, !expr
	}
	return o, nil
}

// fromHEAD finds the branch HEAD was on when branch was created.
func (r *Repo) fromHEAD(o *Origin, branch string, own []reflogEntry, renames map[string]string) error {
	names := []string{branch} // its names, as renames went
	for _, e := range own {
		if from, _, ok := parseRename(e.msg); ok {
			names = append(names, from)
		}
	}
	logs, err := r.headLogs()
	if err != nil {
		return err
	}
	near := func(t time.Time) bool { d := t.Sub(o.At); return d >= -2*time.Second && d <= 2*time.Second }
	// "checkout: moving from P to X", logged in the same second as the creation
	for _, l := range logs {
		for _, e := range l {
			rest, ok := strings.CutPrefix(e.msg, "checkout: moving from ")
			if !ok || !near(e.at) {
				continue
			}
			p, to, ok := strings.Cut(rest, " to ")
			if ok && slices.Contains(names, to) {
				if !isSHA(p) {
					o.Branch = forward(p, renames)
				}
				o.Exact = true
				return nil
			}
		}
	}
	// "git branch X": the branch HEAD was on at the time. With linked worktrees,
	// which one ran the command is unknown.
	// Reflogs count in seconds: a switch in the same second may have come
	// before or after, which makes the answer a guess.
	for _, l := range logs {
		on, sure := "", true
		for _, e := range l {
			if e.at.After(o.At) {
				break
			}
			prev := on
			if rest, ok := strings.CutPrefix(e.msg, "checkout: moving from "); ok {
				_, on, _ = strings.Cut(rest, " to ")
			} else if from, to, ok := parseRename(e.msg); ok && on == from {
				on = to
			}
			if on != prev && e.at.Equal(o.At) {
				sure = false
			}
		}
		if on != "" && on != "HEAD" && !isSHA(on) && !slices.Contains(names, on) {
			o.Branch, o.Exact = forward(on, renames), sure && len(logs) == 1
			return nil
		}
	}
	return nil
}

// GuessParent guesses, from the history, which of candidates branch was
// created from: the one it forked from last. Candidates are local branches or
// "remote/branch" names; nil means every other local branch. Candidates that
// contain branch with more commits are taken as its children, and left out.
// It returns nil when none shares history with it.
//
// Branches at the same commit cannot be told apart: the others are in
// Others. Each candidate costs a walk of the history down to the fork.
func (r *Repo) GuessParent(branch string, candidates []string) (*Origin, error) {
	head, err := r.Resolve(branch)
	if err != nil {
		return nil, err
	}
	if candidates == nil {
		bs, err := r.Branches()
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			candidates = append(candidates, b.Name)
		}
	}
	type scored struct {
		name  string
		fork  *Commit
		still bool // the candidate has not moved since the fork
	}
	var best []scored
	better := func(a, b scored) int { // > 0: a is the better guess
		if c := a.fork.CommitterDate.Compare(b.fork.CommitterDate); c != 0 {
			return c
		}
		switch {
		case a.still && !b.still:
			return 1
		case b.still && !a.still:
			return -1
		}
		return 0
	}
	for _, c := range candidates {
		if c == branch {
			continue
		}
		sha, err := r.Resolve(c)
		if err != nil {
			return nil, err
		}
		fork, err := r.MergeBase(head, sha)
		if err != nil {
			return nil, err
		}
		if fork == nil || (fork.SHA == head && sha != head) {
			continue // unrelated, or a child
		}
		s := scored{name: c, fork: fork, still: sha == fork.SHA}
		switch {
		case len(best) == 0 || better(s, best[0]) > 0:
			best = []scored{s}
		case better(s, best[0]) == 0:
			best = append(best, s)
		}
	}
	if len(best) == 0 {
		return nil, nil
	}
	slices.SortFunc(best, func(a, b scored) int { return strings.Compare(a.name, b.name) })
	o := &Origin{Branch: best[0].name, SHA: best[0].fork.SHA}
	for _, s := range best[1:] {
		o.Others = append(o.Others, s.name)
	}
	return o, nil
}
