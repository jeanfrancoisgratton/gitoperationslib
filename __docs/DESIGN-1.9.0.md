# DESIGN 1.9.0: branch details, local and on the server

Asked on 2026-11-12: everything a "branch ls --extended" could show, in the library, local reads included, so that
every consumer gets it. How a tool presents it (basic list, extended flag) is the tool's business.

## What is wanted

| # | Information | Where it comes from |
|---|---|---|
| 1 | The branch it was created from | local only: the reflog; or a guess from the history (local, or on the server) |
| 2 | Upstream (tracking) branch, ahead/behind it | local: `.git/config`, `refs/remotes`, the commits |
| 3 | Current branch, branches checked out in other worktrees | local: `HEAD`, `.git/worktrees/*/HEAD` |
| 4 | Ahead/behind the default branch | local commits, or the server's `Compare` |
| 5 | Merged into the default branch | the same: ahead = 0 |
| 6 | Last commit's age (stale branches) | local commits, or `LastCommit` (GitHub: `Get` per branch) |
| 7 | Open pull request from the branch | server: `PullRequests().List` with `Head` |
| 8 | CI state of the head commit | server: new, the library has no commit statuses yet |

Not available anywhere: who created a branch, and when (the reflog has when, locally, for 90 days).

## Shape

### A. New package `local`: reading a clone, pure Go, no git client, no new dependency

The library so far only talks to servers; this reads `.git` directly, as gitclt's `shared` package does today.

- `local.Open(dir) (*Repo, error)`: finds `.git` in `dir` or its parents (`.git` files of worktrees and submodules
  too), `ErrNoWorkingCopy` otherwise. Moved from gitclt: `FindGitDir`, `CommonGitDir`, the config reading,
  `ParseRemoteURL`, `InferRepo` (gitclt then drops its copies).
- `Repo.Branches() ([]Branch, error)`: `Name`, `SHA`, `Current`, `Worktree` (path where it is checked out, other
  than this one), `Upstream` (`Remote`, `Branch`, `SHA` from `refs/remotes`, empty when gone).
- `Repo.Remotes()`, `Repo.Current()`, `Repo.DefaultBranch(remote)` (from `refs/remotes/<remote>/HEAD`).
- `Repo.Compare(base, head) (*gol.Comparison, error)`, `Repo.IsAncestor(a, b)`, `Repo.Commit(sha)` (message,
  author, date, parents): needs reading commit objects, see below.
- `Repo.CreatedFrom(branch) (*Origin, error)`: from the reflogs. `Origin{Branch, At, Exact bool}`.
- `Repo.GuessParent(branch, candidates)`: from the history, `Exact` false.

**Reading commits** is the bulk of the work: loose objects (zlib) and packfiles (`.idx` v2, `.pack` with offset and
reference deltas), commits only, SHA-1 repositories only (SHA-256 ones: `ErrNotSupported`). Fresh clones and
`git gc` put everything in packs, so packs are not optional. Walks stop at the merge base; a cap on the walk
(`ErrTooFar`-style) keeps a huge history from hanging a listing. The commit-graph file could speed this later; not
needed to start.

**The reflog, measured with git 2.x:** `git switch -c X` and `git checkout -b X` write `branch: Created from HEAD` in
`logs/refs/heads/X`, and `checkout: moving from P to X` in `logs/HEAD` at the same second; `git branch X P` writes
`Created from P`. So the parent is exact when either names it, and is read from HEAD's log otherwise. Gone after a
fresh clone, after the 90 days of reflog expiry, or when the branch was created by a fetch.

### B. Root package, server side

- New optional interface `CommitStatuses` (`CapCommitStatuses`): the combined state of a ref (`success`,
  `pending`, `failure`, `error`), and the individual statuses (context, state, description, target URL). Forgejo:
  `commits/{ref}/status`; GitHub: combined status plus check runs (Actions reports through check runs only, so
  statuses alone would say nothing); Bitbucket: `commit/{sha}/statuses`, so Bitbucket gets it too.
- A helper building the extended view, so consumers do not each reimplement it:
  `gol.BranchDetails(ctx, client, owner, repo, BranchDetailsOptions{Ahead, PullRequests, Statuses, ...})`
  returning, per branch, the `Branch`, ahead/behind the default, merged, the open pull request(s), the CI state.
  Each costly part is opt-in; the number of requests is documented per forge (GitHub: one `Compare` and, for the
  age, one `Get` per branch; pull requests: one list for the repository; statuses: one or two per branch).
- Parent guess on the server: `Compare` per pair is n² requests; offered only for a given branch against given
  candidates, or left to the local side. To decide.

## Increments

1. **A1:** `local` package: `Open`, branches, upstream, worktrees, remotes, `InferRepo` moved in. Unit tests on
   clones built by the test with the git client (tests only, the library never runs it).
2. **A2:** commit reading (loose, packs, deltas), `Compare`, `IsAncestor`, `Commit`, then `CreatedFrom` and
   `GuessParent`.
3. **B1:** `CommitStatuses` on Forgejo, GitHub, Bitbucket, with live tests.
4. **B2:** `BranchDetails`, docs, then gitclt's memory for its side.

Each stops for review.

## Decided (2026-11-12)

1. Version 1.9.0, released the same day.
2. Package `local`, with a layout refactoring in mind (2.0.0, after this release).
3. Parent: local by default; a client may ask the server, or both. Request cost accepted.
4. As built: `CommitStatuses` also sets statuses (`SetCommitStatus`), for CI tools and the live tests; the combining
   helper is package `branchinfo`, since the root package cannot import `local`.
