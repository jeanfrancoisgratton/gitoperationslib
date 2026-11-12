// Package branchinfo gathers what a detailed branch list shows, from a clone
// (package local), from the server, or both: ahead and behind the default
// branch, merged, the last commit, the branch it was created from, the open
// pull requests and the CI state.
//
// Every part is asked for in Options; what is not asked for costs nothing:
//
//	res, err := branchinfo.Collect(ctx, branchinfo.Options{
//		Local: repo, Client: client, Owner: "acme", Repo: "widget",
//		AheadBehind: branchinfo.Both, Parent: branchinfo.Local, Statuses: true,
//	})
package branchinfo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
	"github.com/jeanfrancoisgratton/gitoperationslib/local"
)

// Source says where a part is read: the clone, the server, or both.
type Source int

const (
	Local  Source = 1 << iota // from the clone, without requests
	Server                    // from the server
	Both   = Local | Server
)

// Options selects the branches and the parts to read.
type Options struct {
	// Local is the clone; nil to read the server alone.
	Local *local.Repo
	// Client, Owner and Repo are the repository on the server; a nil Client
	// reads the clone alone.
	Client      gol.Client
	Owner, Repo string
	// Remote is the clone's remote for that repository; "" means origin. A local
	// branch is paired with the server branch it tracks on this remote, or else
	// with the server branch of the same name.
	Remote string
	// Default is the branch the others are compared with; "" means the
	// server's default branch, or else the remote's as the clone recorded it.
	Default string

	// AheadBehind compares each branch with the default one, which also tells
	// whether it is merged. Server: one request per branch.
	AheadBehind Source
	// LastCommit reads each branch's head commit. Server: free on Forgejo and
	// Bitbucket, one request per branch on GitHub, whose list lacks it.
	LastCommit Source
	// Parent finds the branch each one was created from. Local: the reflogs,
	// else a guess from the history. Server: a guess, one request per pair of
	// branches.
	Parent Source
	// PullRequests reads the open pull requests from each branch (one list for
	// the repository; Forgejo needs version 16).
	PullRequests bool
	// Statuses reads the CI state of each branch's head (CommitStatuses; one
	// to three requests per branch, depending on the forge).
	Statuses bool
}

// Branch is one branch, with the parts asked for. Fields of parts not asked
// for, or not available on that side, are nil.
type Branch struct {
	Name    string `json:"name"` // the local name, or the server's for a branch not in the clone
	Default bool   `json:"default,omitzero"`
	// Local is the branch in the clone; nil when it is not there.
	Local *local.Branch `json:"local,omitzero"`
	// Server is the branch on the server; nil when it is not there.
	Server *gol.Branch `json:"server,omitzero"`

	// Against the default branch: Ahead is what the branch has that the
	// default lacks. Merged is set when Ahead is 0.
	LocalAhead   *gol.Comparison `json:"localAhead,omitzero"`
	LocalMerged  *bool           `json:"localMerged,omitzero"`
	ServerAhead  *gol.Comparison `json:"serverAhead,omitzero"`
	ServerMerged *bool           `json:"serverMerged,omitzero"`

	// LocalCommit is the head commit in the clone; the server's is in
	// Server.LastCommit.
	LocalCommit *local.Commit `json:"localCommit,omitzero"`

	// Origin is where the branch was created from, read in the clone; Exact
	// tells a reflog entry from a guess. ServerParent is the guess from the
	// server's history.
	Origin       *local.Origin `json:"origin,omitzero"`
	ServerParent *local.Origin `json:"serverParent,omitzero"`

	PullRequests []gol.PullRequest   `json:"pullRequests,omitzero"`
	Status       *gol.CombinedStatus `json:"status,omitzero"`
}

// Result is the branches, sorted by name.
type Result struct {
	Default  string   `json:"default"`
	Branches []Branch `json:"branches"`
	// Incomplete is nil when every part was read. Otherwise it describes the
	// parts the server refused (401, 403, 404), the forge cannot read
	// (ErrNotSupported), or the clone lacks (a shallow or partial clone); those
	// fields are left nil. Missing carries the same as data.
	Incomplete error           `json:"-"`
	Missing    []gol.PartError `json:"missing,omitzero"`
}

// Collect reads the branches and the parts asked for.
func Collect(ctx context.Context, opts Options) (*Result, error) {
	if opts.Local == nil && opts.Client == nil {
		return nil, errors.New("branchinfo: neither a clone nor a server")
	}
	if opts.Remote == "" {
		opts.Remote = "origin"
	}
	c := &collector{opts: opts, res: &Result{}}
	if err := c.branches(ctx); err != nil {
		return nil, err
	}
	if err := c.local(); err != nil {
		return nil, err
	}
	if err := c.server(ctx); err != nil {
		return nil, err
	}
	c.res.Incomplete, c.res.Missing = c.parts.Err(), c.parts.Parts()
	return c.res, nil
}

type collector struct {
	opts  Options
	res   *Result
	parts partial.Result
}

// noteLocal records what the clone lacks; anything else fails the call.
func (c *collector) noteLocal(part string, err error) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, local.ErrObjectNotFound) && !errors.Is(err, gol.ErrNotSupported) {
		return err
	}
	return c.parts.Add(part, fmt.Errorf("%w (%w)", gol.ErrNotSupported, err))
}

// branches lists both sides and pairs them.
func (c *collector) branches(ctx context.Context) error {
	o := c.opts
	if o.Default == "" && o.Client != nil {
		r, err := o.Client.Repos().Get(ctx, o.Owner, o.Repo)
		if err != nil {
			return err
		}
		o.Default = r.DefaultBranch
	}
	if o.Default == "" && o.Local != nil {
		def, err := o.Local.DefaultBranch(o.Remote)
		if err != nil {
			return err
		}
		o.Default = def
	}
	c.opts.Default, c.res.Default = o.Default, o.Default

	byServer := map[string]int{} // server branch name → index in res.Branches
	if o.Local != nil {
		bs, err := o.Local.Branches()
		if err != nil {
			return err
		}
		for i := range bs {
			b := &bs[i]
			c.res.Branches = append(c.res.Branches, Branch{Name: b.Name, Local: b})
			serverName := b.Name
			if b.Upstream != nil && b.Upstream.Remote == o.Remote {
				serverName = b.Upstream.Branch
			}
			if _, taken := byServer[serverName]; !taken {
				byServer[serverName] = len(c.res.Branches) - 1
			}
		}
	}
	if o.Client != nil {
		list, err := partial.Collect(ctx, func(ctx context.Context, lo gol.ListOptions) (gol.Page[gol.Branch], error) {
			return o.Client.Branches().List(ctx, o.Owner, o.Repo, lo)
		})
		if err != nil {
			return err
		}
		for i := range list {
			sb := &list[i]
			if j, ok := byServer[sb.Name]; ok {
				c.res.Branches[j].Server = sb
				continue
			}
			c.res.Branches = append(c.res.Branches, Branch{Name: sb.Name, Server: sb})
		}
	}
	for i := range c.res.Branches {
		b := &c.res.Branches[i]
		b.Default = b.Name == o.Default || (b.Server != nil && b.Server.Name == o.Default)
	}
	slices.SortFunc(c.res.Branches, func(a, b Branch) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

// localDefault is the ref the clone compares with: the local default branch,
// or else what the last fetch saw of the remote's.
func (c *collector) localDefault() string {
	for _, b := range c.res.Branches {
		if b.Local != nil && b.Default {
			return b.Local.Name
		}
	}
	if rb, err := c.opts.Local.RemoteBranches(); err == nil {
		if _, ok := rb[c.opts.Remote+"/"+c.opts.Default]; ok {
			return c.opts.Remote + "/" + c.opts.Default
		}
	}
	return ""
}

func (c *collector) local() error {
	o := c.opts
	if o.Local == nil || o.AheadBehind&Local == 0 && o.LastCommit&Local == 0 && o.Parent&Local == 0 {
		return nil
	}
	def := ""
	if o.AheadBehind&Local != 0 {
		if def = c.localDefault(); def == "" {
			_ = c.parts.Add("local ahead/behind", fmt.Errorf("%w: no default branch %q in the clone", gol.ErrNotSupported, o.Default))
		}
	}
	for i := range c.res.Branches {
		b := &c.res.Branches[i]
		if b.Local == nil {
			continue
		}
		if def != "" {
			cmp, err := o.Local.Compare(def, b.Local.SHA)
			if err := c.noteLocal("local ahead/behind of "+b.Name, err); err != nil {
				return err
			}
			if cmp != nil {
				cmp.Head = b.Name
				b.LocalAhead, b.LocalMerged = cmp, gol.Ptr(cmp.Ahead == 0)
			}
		}
		if o.LastCommit&Local != 0 {
			commit, err := o.Local.Commit(b.Local.SHA)
			if err := c.noteLocal("local last commit of "+b.Name, err); err != nil {
				return err
			}
			b.LocalCommit = commit
		}
		if o.Parent&Local != 0 {
			origin, err := o.Local.CreatedFrom(b.Name)
			if err != nil {
				return err
			}
			if origin == nil {
				origin, err = o.Local.GuessParent(b.Name, nil)
				if err := c.noteLocal("local parent of "+b.Name, err); err != nil {
					return err
				}
			}
			b.Origin = origin
		}
	}
	return nil
}

func (c *collector) server(ctx context.Context) error {
	o := c.opts
	if o.Client == nil {
		return nil
	}
	cl := o.Client
	var statuses gol.CommitStatuses
	if o.Statuses {
		var ok bool
		if statuses, ok = cl.(gol.CommitStatuses); !ok {
			_ = c.parts.Add("statuses", gol.NotSupported(cl.Backend(), "statuses.get", "commit statuses"))
		}
	}
	var prs []gol.PullRequest
	if o.PullRequests {
		list, err := partial.Collect(ctx, func(ctx context.Context, lo gol.ListOptions) (gol.Page[gol.PullRequest], error) {
			return cl.PullRequests().List(ctx, o.Owner, o.Repo, gol.ListPROptions{ListOptions: lo})
		})
		if err := c.parts.Add("pull requests", err); err != nil {
			return err
		}
		prs = list
	}
	full := strings.ToLower(o.Owner + "/" + o.Repo)
	for i := range c.res.Branches {
		b := &c.res.Branches[i]
		if b.Server == nil {
			continue
		}
		name := b.Server.Name
		if o.AheadBehind&Server != 0 && o.Default != "" {
			cmp, err := cl.Branches().Compare(ctx, o.Owner, o.Repo, o.Default, name)
			if err := c.parts.Add("ahead/behind of "+name, err); err != nil {
				return err
			}
			if cmp != nil {
				b.ServerAhead, b.ServerMerged = cmp, gol.Ptr(cmp.Ahead == 0)
			}
		}
		if o.LastCommit&Server != 0 && b.Server.LastCommit == nil {
			got, err := cl.Branches().Get(ctx, o.Owner, o.Repo, name)
			if err := c.parts.Add("last commit of "+name, err); err != nil {
				return err
			}
			if got != nil {
				b.Server.LastCommit = got.LastCommit
			}
		}
		for _, pr := range prs {
			if pr.Head.Ref == name && (pr.Head.Repo == "" || strings.ToLower(pr.Head.Repo) == full) {
				b.PullRequests = append(b.PullRequests, pr)
			}
		}
		if statuses != nil {
			st, err := statuses.CommitStatus(ctx, o.Owner, o.Repo, name)
			if err := c.parts.Add("status of "+name, err); err != nil {
				return err
			}
			b.Status = st
		}
	}
	if o.Parent&Server != 0 {
		return c.serverParents(ctx)
	}
	return nil
}

// serverParents guesses each branch's parent with the server's comparisons:
// the candidate it has the fewest commits beyond, which does not contain it
// with more commits (a child), then the one that moved least since.
func (c *collector) serverParents(ctx context.Context) error {
	o := c.opts
	var names []string
	for _, b := range c.res.Branches {
		if b.Server != nil {
			names = append(names, b.Server.Name)
		}
	}
	for i := range c.res.Branches {
		b := &c.res.Branches[i]
		if b.Server == nil {
			continue
		}
		type scored struct {
			name          string
			ahead, behind int
		}
		var best []scored
		better := func(a, b scored) int { return cmp.Or(cmp.Compare(b.ahead, a.ahead), cmp.Compare(b.behind, a.behind)) }
		for _, cand := range names {
			if cand == b.Server.Name {
				continue
			}
			cmpr, err := o.Client.Branches().Compare(ctx, o.Owner, o.Repo, cand, b.Server.Name)
			if gol.IsNotFound(err) {
				continue // unrelated histories
			}
			if err := c.parts.Add("parent of "+b.Server.Name, err); err != nil {
				return err
			}
			if cmpr == nil {
				break // refused: the others would be too
			}
			if cmpr.Ahead == 0 && cmpr.Behind > 0 {
				continue // a child
			}
			s := scored{cand, cmpr.Ahead, cmpr.Behind}
			switch {
			case len(best) == 0 || better(s, best[0]) > 0:
				best = []scored{s}
			case better(s, best[0]) == 0:
				best = append(best, s)
			}
		}
		if len(best) == 0 {
			continue
		}
		slices.SortFunc(best, func(a, b scored) int { return strings.Compare(a.name, b.name) })
		p := &local.Origin{Branch: best[0].name}
		for _, s := range best[1:] {
			p.Others = append(p.Others, s.name)
		}
		b.ServerParent = p
	}
	return nil
}
