package local

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ErrNoWorkingCopy is returned when a directory is not part of a git working
// copy.
var ErrNoWorkingCopy = errors.New("not inside a git working copy")

// Repo is a working copy, or a bare repository.
//
// A Repo is safe for concurrent use. Reading commits opens the pack files,
// which stay open until Close.
type Repo struct {
	gitDir    string // this working copy's: a linked worktree has its own
	commonDir string // the one holding branches and config, shared by worktrees

	mu      sync.Mutex
	store   *objectStore
	shallow map[string]bool
}

// Open finds the working copy dir is part of: dir itself, or one of its
// parents. A bare repository is opened by its own directory.
func Open(dir string) (*Repo, error) {
	gitDir, err := findGitDir(dir)
	if err != nil {
		return nil, err
	}
	return &Repo{gitDir: gitDir, commonDir: commonGitDir(gitDir)}, nil
}

// GitDir is the .git directory of this working copy.
func (r *Repo) GitDir() string { return r.gitDir }

// findGitDir looks for .git in dir and its parents; .git is a directory, or a
// file pointing to one (worktrees, submodules).
func findGitDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if isGitDir(dir) {
		return dir, nil // a bare repository, or a .git directory itself
	}
	for {
		p := filepath.Join(dir, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return p, nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			if target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: "); ok {
				return resolvePath(dir, target), nil
			}
			return "", errors.New(p + " is neither a directory nor a gitdir link")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoWorkingCopy
		}
		dir = parent
	}
}

func isGitDir(dir string) bool {
	for _, p := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			return false
		}
	}
	return true
}

// commonGitDir returns the directory holding the branches and the config: a
// linked worktree shares them with the main working copy.
func commonGitDir(gitDir string) string {
	if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		return resolvePath(gitDir, strings.TrimSpace(string(b)))
	}
	return gitDir
}

func resolvePath(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(base, p))
}

func (r *Repo) config() (config, error) {
	return readConfig(filepath.Join(r.commonDir, "config"))
}

// Current is the branch checked out here; "" on a detached HEAD.
func (r *Repo) Current() (string, error) {
	return headBranch(filepath.Join(r.gitDir, "HEAD"))
}

func headBranch(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
	if name == strings.TrimSpace(string(b)) {
		return "", nil // a commit SHA: detached
	}
	return name, nil
}

// refs returns the refs under prefix ("refs/heads/") by their name after it,
// with their commit: packed ones, overridden by loose ones. Symbolic refs
// (refs/remotes/origin/HEAD) are left out.
func (r *Repo) refs(prefix string) (map[string]string, error) {
	refs := map[string]string{}
	f, err := os.Open(filepath.Join(r.commonDir, "packed-refs"))
	if err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			sha, ref, ok := strings.Cut(s.Text(), " ")
			if !ok {
				continue // comments, and "^<sha>" peeled tag lines, have no space
			}
			if name, ok := strings.CutPrefix(ref, prefix); ok {
				refs[name] = sha
			}
		}
		err = s.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	root := filepath.Join(r.commonDir, filepath.FromSlash(prefix))
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		v := strings.TrimSpace(string(b))
		if strings.HasPrefix(v, "ref: ") {
			return nil
		}
		name, _ := filepath.Rel(root, path)
		refs[filepath.ToSlash(name)] = v
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return refs, nil
}

// Branch is a local branch.
type Branch struct {
	Name string `json:"name"`
	SHA  string `json:"sha"` // head commit
	// Current is the branch checked out in the working copy that was opened.
	Current bool `json:"current,omitzero"`
	// Worktree is where the branch is checked out, this working copy or
	// another of the same repository; empty when it is not.
	Worktree string `json:"worktree,omitzero"`
	// Upstream is the branch it tracks; nil when it tracks none.
	Upstream *Upstream `json:"upstream,omitzero"`
}

// Upstream is the branch a local branch tracks.
type Upstream struct {
	Remote string `json:"remote"` // "." for another local branch
	Branch string `json:"branch"` // its name on the remote
	// SHA is what the last fetch saw of it; empty when it is gone from the
	// remote (pruned) or was never fetched.
	SHA string `json:"sha,omitzero"`
}

// Branches lists the local branches, sorted by name.
func (r *Repo) Branches() ([]Branch, error) {
	heads, err := r.refs("refs/heads/")
	if err != nil {
		return nil, err
	}
	cfg, err := r.config()
	if err != nil {
		return nil, err
	}
	current, err := r.Current()
	if err != nil {
		return nil, err
	}
	trees, err := r.worktrees(cfg)
	if err != nil {
		return nil, err
	}
	var remoteRefs map[string]string
	out := make([]Branch, 0, len(heads))
	for name, sha := range heads {
		b := Branch{Name: name, SHA: sha, Current: name == current, Worktree: trees[name]}
		remote, merge := cfg.get("branch."+name+".remote"), cfg.get("branch."+name+".merge")
		if remote != "" && merge != "" {
			up := &Upstream{Remote: remote, Branch: strings.TrimPrefix(merge, "refs/heads/")}
			if remote == "." {
				up.SHA = heads[up.Branch]
			} else {
				if remoteRefs == nil {
					if remoteRefs, err = r.refs("refs/remotes/"); err != nil {
						return nil, err
					}
				}
				up.SHA = remoteRefs[remote+"/"+up.Branch]
			}
			b.Upstream = up
		}
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b Branch) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// worktrees maps each checked out branch to the directory it is checked out in.
func (r *Repo) worktrees(cfg config) (map[string]string, error) {
	out := map[string]string{}
	if cfg.get("core.bare") != "true" {
		if name, err := headBranch(filepath.Join(r.commonDir, "HEAD")); err == nil && name != "" {
			out[name] = filepath.Dir(r.commonDir)
		}
	}
	dirs, err := os.ReadDir(filepath.Join(r.commonDir, "worktrees"))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		admin := filepath.Join(r.commonDir, "worktrees", d.Name())
		name, err := headBranch(filepath.Join(admin, "HEAD"))
		if err != nil || name == "" {
			continue
		}
		// gitdir holds the path of the worktree's .git file.
		b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		out[name] = filepath.Dir(resolvePath(admin, strings.TrimSpace(string(b))))
	}
	return out, nil
}

// RemoteBranches lists what the last fetch saw of the remotes' branches, as
// "remote/branch" names with their commits.
func (r *Repo) RemoteBranches() (map[string]string, error) {
	return r.refs("refs/remotes/")
}

// DefaultBranch is a remote's default branch, as recorded at clone time (or by
// "git remote set-head"); "" when it was not.
func (r *Repo) DefaultBranch(remote string) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.commonDir, "refs", "remotes", remote, "HEAD"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/remotes/"+remote+"/")
	return name, nil
}
