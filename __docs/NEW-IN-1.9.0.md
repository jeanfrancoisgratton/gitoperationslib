# gitoperationslib 1.9.0: what is new

Detailed branch information, from a clone and from the server. Full guide: `__docs/USAGE.md` §25–27 in the library.

| Feature | Where | Forgejo | GitHub | Bitbucket | Cost | Notes |
|---|---|---|---|---|---|---|
| Open a clone, from any directory inside it | `local.Open(dir)` | — | — | — | none (reads `.git`) | Linked worktrees and submodules followed; no git client needed |
| Local branches: head, current, worktree, upstream and its fetched commit | `repo.Branches()` | — | — | — | none | `Upstream.SHA` empty when the remote branch is gone |
| Remotes, the remote's default branch, remote branches | `Remotes`, `DefaultBranch(remote)`, `RemoteBranches` | — | — | — | none | |
| The server repository the clone points to | `repo.RepoOn(serverURL)`, `MatchesRepo` | — | — | — | none | Replaces gitclt's `shared.InferRepo` |
| Commits, ahead/behind, merged, merge base | `Commit`, `Compare`, `IsAncestor`, `MergeBase` | — | — | — | none | Loose objects and packs; matches git on 72,000 commits |
| Branch it was created from, exact | `repo.CreatedFrom(branch)` | — | — | — | none | From the reflogs: this clone only, 90 days by default |
| Branch it was created from, guessed | `repo.GuessParent(branch, candidates)` | — | — | — | none | Ties (same commit) in `Origin.Others` |
| A commit's combined CI state and statuses | `gol.CommitStatuses.CommitStatus` | ✅ | ✅ statuses + check runs | ✅ | 1–3 requests | `CapCommitStatuses` |
| Report a status on a commit | `SetCommitStatus` | ✅ incl. warning | ✅ statuses only | ✅ needs a target URL | 1 request | |
| Detailed branch list, local, server or both | `branchinfo.Collect` | ✅ | ✅ | ✅ | per part, below | Each part is opt-in |
| · ahead/behind the default branch, merged | `AheadBehind: Local/Server/Both` | ✅ | ✅ | ✅ | server: 1 per branch | |
| · last commit | `LastCommit` | ✅ free | ✅ 1 per branch | ✅ free | | |
| · parent branch | `Parent` | ✅ | ✅ | ✅ | local: none; server: 1 per pair of branches | Local is the reflog, then a guess |
| · open pull requests from the branch | `PullRequests: true` | ✅ | ✅ | ✅ | 1 list per repository | Forks left out |
| · CI state of the head | `Statuses: true` | ✅ | ✅ | ✅ | 1–3 per branch | |
| · parts refused or missing | `Result.Missing` | | | | | Shown as notes, not errors |
