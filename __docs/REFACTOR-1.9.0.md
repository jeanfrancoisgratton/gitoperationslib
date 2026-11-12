# gitoperationslib 1.9.0: layout refactoring

**Status:** implemented on 2026-11-12, uncommitted when written; live suites passed on Forgejo 15 and 17. Where the build differs from the proposal, the grids below say what was built, and "As built" lists the differences.
Part of 1.9.0, with the branch details: the module path does not change. Consumers: gitclt and stubber only, both the user's; they are migrated after the library.

## Why

Measured on 1.9.0, the layout grew one release at a time:

- **Two ways to reach a feature.** 12 areas are accessors on `Client` (`client.Branches()`), 27 more are optional
  interfaces found by type assertion (`client.(gol.Runners)`), some of them two levels deep
  (`client.(gol.SiteHooks).SystemWebhooks()`, `client.Releases().(gol.ExternalAssets)`).
- **One area, several places.** Commits are in `History`, `CommitDiffs` and `CommitStatuses`; pull requests in
  `PullRequests`, `PRContents` and `ReviewComments`; labels in `Labels`, `LabelsByID` and `OrgLabels`; webhooks in
  five interfaces over two files.
- **Overlap.** `Directory` (reads, all forges) and `Admin` (writes, Forgejo) both cover users, organisations and teams:
  `Directory.Org` and `Admin.Orgs().Get` do the same thing.
- **Files don't match areas.** `client.go` holds 19 interfaces; `branch.go` holds tags; `runner.go` holds log following;
  backend files are named differently in each backend (`refs.go`, `diffs.go`, `me.go`, `deliveries.go`).
- **Method names repeat their area** where they were bolted on: `ListRunners`, `ListWorkflowRuns`, `LockIssue`,
  `SetOrgAvatar`, `ListCollaborators`, against `Branches().List`.

## Decisions to confirm

| # | Proposal | Alternative | Why |
|---|---|---|---|
| D1 | **Decided by the user:** module path unchanged, everything in 1.9.0 | A `/v2` module | Breaking a minor version goes against semantic versioning, knowingly: the library has no users but the user's own tools |
| D2 | **One root package** for types and interfaces, one file per area | A package per area (`repo`, `issue`, …) | Areas share types (`Repo`, `User`, `Page`, `PartError`); per-area packages need a base package and many imports in consumers. go-github does the same |
| D3 | **Every area is an accessor**, never a type assertion; what a backend lacks returns `ErrNotSupported` from each method. `Capabilities()` stays, to check without calling | Keep optional interfaces | One way to reach everything, visible in autocompletion; `ErrNotSupported` is already how version-dependent features answer |
| D4 | **`Directory` and `Admin` merge** into `Users()`, `Orgs()`, `Teams()`; reads work on all forges, writes where they exist. Teams addressed by organisation and name everywhere | Keep both | Removes the overlap; GitHub addresses teams by name (slug), Forgejo's IDs stay readable in `Team.ID` |
| D5 | Same method names in every service: `List`, `Get`, `Create`, `Edit`, `Delete`, plus verbs (`Merge`, `Lock`, `Sync`); no area prefix | — | `BranchProtections.Update` becomes `Edit`, as everywhere else |
| D6 | Unchanged: JSON tags, error sentinels and `APIError`, backend constructors and options, `Capability` names and values, the `webhook`, `local` and `branchinfo` packages | — | Keeps gitclt's `--json` output and error handling as they are |

## Target `Client` (1.9.0)

```go
type Client interface {
	Backend() Backend
	Capabilities() Capability

	Repos() Repos                 // Get List Create Edit Delete
	Branches() Branches           // List Get Create Rename Delete Compare
	Tags() Tags                   // List Get Create Delete
	BranchProtections() BranchProtections // List Get Create Edit Delete
	TagProtections() TagProtections
	Commits() Commits             // List Get Diff Status SetStatus
	Collaborators() Collaborators // List Get Add Remove ListInvitations
	Mirrors() Mirrors             // ListPush GetPush CreatePush DeletePush SyncPush Sync
	GitHooks() GitHooks           // List Get Set Delete
	Webhooks() Webhooks           // repo and org scopes; List Get Create Edit Delete Ping ListDeliveries GetDelivery Redeliver
	Releases() Releases           // … plus AddExternalAsset

	PullRequests() PullRequests   // … plus Diff Files Commits ListReviewComments CreateReviewComment DeleteReviewComment
	Issues() Issues               // … plus Lock Unlock
	Labels() Labels               // repository labels; plus EditByID DeleteByID
	Milestones() Milestones

	CI() CI                       // Secrets() Variables() Runs() Artifacts() Runners()

	Account() Account             // Emails() SSHKeys() GPGKeys() Profile() Tokens() Webhooks()
	Users() Users                 // Me Get Info List Search Create Edit Rename Delete Tokens(login)
	Orgs() Orgs                   // Get Info List ListAll Create Edit Rename Delete Members IsMember RemoveMember SetAvatar DeleteAvatar Labels()
	Teams() Teams                 // List Get Info Create Edit Delete Members IsMember AddMember RemoveMember Repos AddRepo RemoveRepo
	Site() Site                   // SystemWebhooks() DefaultWebhooks()  (instance administration)
}
```

## Target layout

```
gitoperationslib/
├── client.go        Client only
├── errors.go  enums.go  pagination.go  retry.go  capabilities.go  ptr.go
├── repos.go  branches.go  tags.go  protections.go  commits.go  collaborators.go  mirrors.go  githooks.go
├── hooks.go  releases.go  pulls.go  issues.go  labels.go  milestones.go
├── ci.go  runs.go  runners.go
├── account.go  users.go  orgs.go  teams.go  site.go
├── forgejo/  github/  bitbucket/      one file per area, same names as the root files
├── webhook/  local/  branchinfo/      unchanged but for the import path
└── internal/
    ├── forgejoapi/  bitbucketapi/  partial/  refusal/  retry/  features/   unchanged
    ├── unsupported/   new: a base per service answering ErrNotSupported, which backends embed
    └── contracttest/  one file per area, same names
```

## As built

- **Capabilities** keep their bit values: bits were only ever appended, and nothing needed them renumbered.
- **Backend files** keep their names (Grid C was not applied): only the methods moved onto the services. Renaming
  them is cosmetic and can follow later.
- **Bitbucket's `unsupported.go`** stays, returning the generated services with Bitbucket's reasons.
- **Every service is a `struct{ c *Client }`**: methods moved from `*Client` open with `c := svc.c`, which keeps their
  bodies unchanged.
- **The live tests are unchanged in what they check**: contract tests skip on a missing capability instead of a failed
  type assertion.

## Grid A: root package, file by file

| Source (1.8.0) | Target (1.9.0) | What moves | Notes |
|---|---|---|---|
| `client.go` | `client.go` | `Client` | Rewritten as above |
| `client.go` | `repos.go` | `Repos` | Joins `Repo`, its options and settings |
| `client.go` | `branches.go` | `Branches` | |
| `client.go` | `tags.go` | `Tags` | |
| `client.go` | `protections.go` | `BranchProtections`, `TagProtections` | `Update` → `Edit` |
| `client.go` | `pulls.go` | `PullRequests` | |
| `client.go` | `issues.go` | `Issues` | |
| `client.go` | `labels.go` | `Labels` | |
| `client.go` | `milestones.go` | `Milestones` | |
| `client.go` | `hooks.go` | `Webhooks` | |
| `client.go` | `releases.go` | `Releases`, `ExternalAssets` | `ExternalAssets` merges into `Releases` |
| `client.go` | `users.go`, `orgs.go`, `teams.go` | `Admin`, `Users`, `UserFilters`, `Orgs`, `OrgAvatars`, `Teams` | `Admin` removed; `UserFilters` → `Users().Search`; `OrgAvatars` → `Orgs().SetAvatar`, `DeleteAvatar` |
| `repo.go` | `repos.go` | `Repo`, `CreateRepoOptions`, `EditRepoOptions`, `RepoFeatures`, settings types | `Ptr` → `ptr.go` |
| `branch.go` | `branches.go` | `Branch`, `CommitSummary`, `Comparison`, `CreateBranchOptions` | |
| `branch.go` | `tags.go` | `Tag`, `CreateTagOptions` | |
| `protection.go` | `protections.go` | `Allowlist`, `BranchProtection`, `TagProtection` | |
| `commit.go` | `commits.go` | `History`, `Commits`, `ListCommitsOptions`, `Commit`, `CommitAuthor`, `CommitStats`, `ChangedFile`, `FileStatus` | `History` removed: `client.Commits()` |
| `diff.go` | `commits.go` | `CommitDiffs`, `DiffFormat` | `CommitDiff` → `Commits().Diff` |
| `diff.go` | `pulls.go` | `PRContents`, `ReviewComments`, `ReviewComment`, `CreateReviewCommentOptions` | Merge into `PullRequests` |
| `status.go` | `commits.go` | `CommitStatuses`, `StatusState`, `CombinedStatus`, `CommitStatus`, `CombineStates` | `CommitStatus` → `Commits().Status`, `SetCommitStatus` → `SetStatus` |
| `pullrequest.go` | `pulls.go` | all types | |
| `issue.go` | `issues.go` | issues, comments, `IssueLocks`, `LockReason` | `IssueLocks` merges into `Issues` |
| `issue.go` | `labels.go` | `Label`, `EditLabelOptions`, `LabelsByID`, `OrgLabels` | `LabelsByID` merges into `Labels`; `OrgLabels` → `Orgs().Labels()` |
| `issue.go` | `milestones.go` | milestone types | |
| `release.go` | `releases.go` | all types | |
| `hook.go` | `hooks.go` | `HookScope`, `HookEvent`, `HookType`, webhook types, `HookDeliveries` | `HookDeliveries` merges into `Webhooks` |
| `hook.go` | `account.go` | `UserHooks`, `UserWebhooks` | → `Account().Webhooks()` |
| `hook.go` | `site.go` | `SiteHooks`, `SystemWebhooks`, `DefaultWebhooks` | → `Site().SystemWebhooks()`, `Site().DefaultWebhooks()` |
| `collaborator.go` | `collaborators.go` | all, `CollaboratorInvitations` | Invitations merge in |
| `mirror.go` | `mirrors.go` | all | |
| `githook.go` | `githooks.go` | all | |
| `ci.go` | `ci.go` | `CI`, `CIScope`, scopes, `Secrets`, `Variables`, `OrgSecrets` | `CI` gains `Runs()`, `Artifacts()`, `Runners()`; `OrgSecrets` merges into `Secrets` |
| `workflow.go` | `runs.go` | `WorkflowRuns`, `WorkflowArtifacts`, their types | → `CI().Runs()`, `CI().Artifacts()` |
| `runner.go` | `runners.go` | `Runners`, scopes, types | → `CI().Runners()` |
| `runner.go` | `runs.go` | `WorkflowLogFollower`, `FollowLogOptions` | Merges into `CI().Runs()` as `FollowJobLog` |
| `account.go` | `account.go` | `Account`, `Emails`, `SSHKeys`, `GPGKeys`, `Profile`, `Tokens`, token types | |
| `account.go` | `users.go` | `AdminTokens` | `TokensOf(login)` → `Users().Tokens(login)` |
| `directory.go` | `users.go`, `orgs.go`, `teams.go` | `Directory`, `UserInfo`, `CurrentUser`, `OrgInfo`, `TeamInfo` | `Directory` removed, see Grid B |
| `directory.go` | `errors.go` | `PartError` | |
| `user.go`, `org.go`, `team.go` | `users.go`, `orgs.go`, `teams.go` | all types | |
| `errors.go`, `enums.go`, `pagination.go`, `retry.go`, `capabilities.go`, `doc.go` | same | | `Capability` bits regrouped by area |

## Grid B: how consumers reach each feature

| 1.8.0 | 1.9.0 | Notes |
|---|---|---|
| `client.(gol.History).Commits().List/Get` | `client.Commits().List/Get` | |
| `client.(gol.CommitDiffs).CommitDiff` | `client.Commits().Diff` | |
| `client.(gol.CommitStatuses).CommitStatus`, `SetCommitStatus` | `client.Commits().Status`, `SetStatus` | |
| `client.(gol.PRContents).PullRequestDiff`, `PullRequestFiles`, `PullRequestCommits` | `client.PullRequests().Diff`, `Files`, `Commits` | |
| `client.(gol.ReviewComments).ListReviewComments` … | `client.PullRequests().ListReviewComments` … | Names kept: they read well in `PullRequests` |
| `client.(gol.IssueLocks).LockIssue`, `UnlockIssue` | `client.Issues().Lock`, `Unlock` | |
| `client.(gol.LabelsByID).EditLabelByID`, `DeleteLabelByID` | `client.Labels().EditByID`, `DeleteByID` | |
| `client.(gol.OrgLabels).ListOrgLabels` … | `client.Orgs().Labels().List` … | `List/Get/Create/Edit/Delete(org, …)` |
| `client.Releases().(gol.ExternalAssets).AddExternalAsset` | `client.Releases().AddExternalAsset` | |
| `client.(gol.Collaborators).ListCollaborators` … | `client.Collaborators().List/Get/Add/Remove` | |
| `client.(gol.CollaboratorInvitations).ListCollaboratorInvitations` | `client.Collaborators().ListInvitations` | |
| `client.(gol.Mirrors).ListPushMirrors` … `SyncMirror` | `client.Mirrors().ListPush` … `Sync` | |
| `client.(gol.GitHooks).ListGitHooks` … | `client.GitHooks().List/Get/Set/Delete` | |
| `client.(gol.HookDeliveries).ListHookDeliveries`, `GetHookDelivery`, `RedeliverHook` | `client.Webhooks().ListDeliveries`, `GetDelivery`, `Redeliver` | |
| `client.(gol.UserHooks).UserWebhooks()` | `client.Account().Webhooks()` | The authenticated user's hooks |
| `client.(gol.SiteHooks).SystemWebhooks()`, `DefaultWebhooks()` | `client.Site().SystemWebhooks()`, `DefaultWebhooks()` | |
| `client.(gol.CI).Secrets()`, `Variables()` | `client.CI().Secrets()`, `Variables()` | |
| `client.(gol.OrgSecrets).GetOrgSecretAccess` … | `client.CI().Secrets().GetAccess`, `SetWithAccess`, `SetRepos` | Org-limited secrets, GitHub |
| `client.(gol.WorkflowRuns).ListWorkflowRuns` … `WorkflowRunLogs` | `client.CI().Runs().List`, `Get`, `Cancel`, `Rerun`, `Delete`, `ListJobs`, `GetJob`, `RerunJob`, `JobLogs`, `Logs` | |
| `client.(gol.WorkflowLogFollower).FollowWorkflowJobLog` | `client.CI().Runs().FollowJobLog` | |
| `client.(gol.WorkflowArtifacts).ListWorkflowArtifacts` … | `client.CI().Artifacts().List`, `ListForRun`, `Get`, `Delete`, `Download` | |
| `client.(gol.Runners).ListRunners` … `RunnerRegistrationToken` | `client.CI().Runners().List`, `Get`, `Register`, `Delete`, `RegistrationToken` | |
| `client.(gol.Account).Emails()` … | `client.Account().Emails()` … | |
| `client.(gol.AdminTokens).TokensOf(login)` | `client.Users().Tokens(login)` | Site admins |
| `client.Directory().Me()` | `client.Users().Me()` | |
| `client.Directory().User`, `UserInfo` | `client.Users().Get`, `Info` | `Get` was in both |
| `client.(gol.Admin).Users().List/Create/Edit/Rename/Delete` | `client.Users().List/Create/Edit/Rename/Delete` | |
| `client.(gol.UserFilters).ListUsersBy` | `client.Users().Search` | |
| `client.Directory().Org`, `ListOrgs`, `ListAllOrgs`, `OrgInfo`, `OrgMembers` | `client.Orgs().Get`, `List`, `ListAll`, `Info`, `Members` | |
| `client.(gol.Admin).Orgs().*` | `client.Orgs().*` | |
| `client.(gol.OrgAvatars).SetOrgAvatar`, `DeleteOrgAvatar` | `client.Orgs().SetAvatar`, `DeleteAvatar` | |
| `client.Directory().ListTeams`, `Team`, `TeamInfo`, `TeamMembers`, `TeamRepos` | `client.Teams().List`, `Get`, `Info`, `Members`, `Repos` | By organisation and name |
| `client.(gol.Admin).Teams().*(id …)` | `client.Teams().*(org, name …)` | Forgejo looks the ID up; `GetByName` removed |
| `client.BranchProtections().Update`, `TagProtections().Update` | `…().Edit` | |
| everything else on `Client` | unchanged | `Repos`, `Branches`, `Tags`, `PullRequests`, `Issues`, `Labels`, `Milestones`, `Releases`, `Webhooks` |
| `if x, ok := client.(gol.Y); ok` | call it; `errors.Is(err, gol.ErrNotSupported)`, or `client.Capabilities().Has(gol.CapY)` first | The pattern every consumer changes |

## Grid C: backend packages

Each backend gets the root's file names; the content is moved, then renamed per Grid B.

| Source | Target | Notes |
|---|---|---|
| `forgejo/branches.go` | `forgejo/branches.go`, `forgejo/tags.go` | Tags split out |
| `forgejo/protection.go` | `forgejo/protections.go` | |
| `forgejo/commits.go`, `forgejo/statuses.go`, `forgejo/diffs.go` (commit part) | `forgejo/commits.go` | |
| `forgejo/pulls.go`, `forgejo/diffs.go` (pull request part) | `forgejo/pulls.go` | |
| `forgejo/issues.go` | `forgejo/issues.go`, `forgejo/labels.go`, `forgejo/milestones.go` | |
| `forgejo/orglabels.go` | `forgejo/labels.go` | |
| `forgejo/hooks.go` | `forgejo/hooks.go`, `forgejo/site.go` | System and default hooks to `site.go` |
| `forgejo/workflows.go` | `forgejo/runs.go` | |
| `forgejo/runners.go`, `forgejo/ci.go` | same | |
| `forgejo/directory.go`, `forgejo/me.go`, `forgejo/users.go`, `forgejo/orgs.go`, `forgejo/teams.go` | `forgejo/users.go`, `forgejo/orgs.go`, `forgejo/teams.go` | Directory reads merge into each |
| `forgejo/account.go`, `collaborators.go`, `mirrors.go`, `githooks.go`, `releases.go`, `repos.go`, `client.go`, `version.go` | same | |
| `github/branches.go` | `github/branches.go`, `github/tags.go` | |
| `github/rulesets.go` | `github/protections.go` | Branch and tag rulesets |
| `github/commits.go`, `github/statuses.go`, `github/diffs.go` (commit part) | `github/commits.go` | |
| `github/pulls.go`, `github/diffs.go` (pull request part) | `github/pulls.go` | |
| `github/issues.go` | `github/issues.go`, `github/labels.go`, `github/milestones.go` | |
| `github/hooks.go`, `github/deliveries.go` | `github/hooks.go` | |
| `github/workflows.go` | `github/runs.go` | |
| `github/directory.go`, `github/me.go` | `github/users.go`, `github/orgs.go`, `github/teams.go` | |
| `github/graphql.go` | same | Backend-private GraphQL client |
| `github/account.go`, `ci.go`, `collaborators.go`, `releases.go`, `repos.go`, `runners.go`, `client.go` | same | |
| `bitbucket/refs.go` | `bitbucket/branches.go`, `bitbucket/tags.go` | |
| `bitbucket/protection.go` | `bitbucket/protections.go` | |
| `bitbucket/commits.go`, `bitbucket/statuses.go`, `bitbucket/diffs.go` (commit part) | `bitbucket/commits.go` | |
| `bitbucket/pulls.go`, `bitbucket/diffs.go` (pull request part) | `bitbucket/pulls.go` | |
| `bitbucket/me.go` | `bitbucket/users.go` | |
| `bitbucket/unsupported.go` | same | Now returns `internal/unsupported` services, each with Bitbucket's reason |
| `bitbucket/account.go`, `ci.go`, `collaborators.go`, `hooks.go`, `repos.go`, `client.go` | same | |
| `*_test.go`, `*_integration_test.go` | follow their file | `post15_*`, `paging_*` and the like stay as they are |

## Grid D: other packages

| Source | Target | Notes |
|---|---|---|
| `internal/unsupported/` | new | Generated (`go generate`): one struct per service, `{Backend, Reason}`, every method answering `NotSupported(backend, op, reason)` with snake_case operations. A backend returns it, or embeds it and implements what it can |
| `internal/contracttest/` | same, files renamed to the areas | Assertions removed: each check calls and skips on `ErrNotSupported` |
| `internal/forgejoapi`, `bitbucketapi`, `partial`, `refusal`, `retry`, `features` | same | |
| `webhook/`, `local/`, `branchinfo/` | same | `branchinfo` follows the new names |
| `__docs/USAGE.md` | rewritten per area | Sections follow the target `Client`; `support_grid.md` and `README.md` follow |

## Grid E: consumers

| Consumer | Use (measured 2026-11-12) | What changes | Effort |
|---|---|---|---|
| stubber (`/localrepos/mainline/stubber`, on 1.7.0) | `gol.Client`, `CreateRepoOptions`, `EditRepoOptions`, `VisibilityPrivate`, `Ptr`, `IsNotFound`, `StatusCode`, the three constructors | Nothing but the version: every name it uses stays | Bump only |
| gitclt | 106 files import the library; 15 kinds of type assertion; `Directory()` and `Admin()` throughout the user, org and team commands | Every assertion → accessor (Grid B); `Directory`/`Admin` → `Users`/`Orgs`/`Teams`; teams by name; method renames in the run, runner, mirror, collaborator, git hook and label commands; `shared/gitrepo.go` and `workingcopy.go` → `local` (from 1.9.0) | Large but mechanical: the compiler finds every site |

## Order of work

1. **R1, root package:** files and interfaces as in Grids A and B, `internal/unsupported`. Stop for review.
2. **R2, backends:** Forgejo, then GitHub, then Bitbucket (Grid C); unit tests pass. Stop for review.
3. **R3, tests and docs:** contract tests, live runs on Forgejo 15 and 17, GitHub, Bitbucket; USAGE, support grid,
   README, CHANGELOG with a migration section built from Grid B. Tag 1.9.0 when the user says.
4. **R4, gitclt**, then **R5, stubber**, each in its own session.
