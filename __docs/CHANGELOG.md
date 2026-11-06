# Changelog

All notable changes to **gitoperationslib** are documented in this file.

The format is based on Keep a Changelog (keepachangelog.com, version 1.1.0), and this project
adheres to Semantic Versioning (semver.org, version 2.0.0). Before 1.0.0, minor versions may
change the public API.

## [1.5.0] — 2026-11-05

What Forgejo's API gained after 15, from `__docs/post-v15.md`, except workflow runs, which are deferred (item 100).

### Added
- **`UserFilters`** (`CapUserFilters`, Forgejo only): `ListUsersBy` lists users with or without two-factor authentication.
  It needs Forgejo 16, and returns `ErrNotSupported` below it, as older servers ignore the filter and return everyone.
- `Secrets().List` on `UserCI` works from Forgejo 17, and still returns `ErrNotSupported` below it.
- New fields: `AccessToken.CreatedAt`, `Org.CreatedAt`, `SSHKey.UpdatedAt` and `SSHKey.Verified`, from Forgejo 16 and zero
  below it. `Org.CreatedAt` and `SSHKey.Verified` are also set on GitHub.
- `ListPROptions.Base` and `Head` select pull requests by branch, on all three forges. Forgejo needs version 16, and
  returns `ErrNotSupported` below it, as older servers ignore them and return every pull request. `"user:branch"`, for a
  fork, works on GitHub only.
- **`IssueLocks`** (`CapIssueLocks`, Forgejo 17 and GitHub): `LockIssue` and `UnlockIssue`, with `LockReason`. Locking a
  locked issue and unlocking an unlocked one succeed on both.
- `ExtraLines` on `CreateReviewCommentOptions` and `ReviewComment`: comments on several lines, on all three forges, and in
  `Event.ReviewComment`. Forgejo needs version 16, and returns `ErrNotSupported` below it, as older servers ignore it.
- **`AdminTokens`** (`CapAdminTokens`, Forgejo 16): `TokensOf(login)` manages another user's access tokens with an ordinary
  admin token, where `Tokens()` needs that user's password.
- `ErrInvitationPending`.

### Changed
- `Branches().Compare` on Forgejo asks the server not to list files and check signatures, as it only needs a count.

### Fixed
- **`Teams().AddMember` reported success for a user who had only been invited.** From Forgejo 16, on an instance with
  `ADD_MEMBERS_BY_INVITATIONS`, the server answers 204 for an invitation, and 409 for a second attempt. `AddMember` now
  checks the result, which takes one more request from 16 on, and returns an error matching `ErrInvitationPending`.

## [1.4.0] — 2026-11-05

Forgejo 17's workflow webhook events, creating system webhooks, webhook types, typed workflow deliveries, and fixes to
Forgejo webhooks that were checked for the first time against a server's real behaviour.

### Added
- **`SystemWebhooks.Create`.** It relies on a flag Forgejo's API does not document, compared against a webhook made in the
  web interface on 15.0.11 and 17.0.1, and checks the result: if it got a default webhook, it deletes it and returns an
  error. This adds a method to an interface released in 1.3.0, an exception to the rule that new features arrive as
  optional interfaces: `SiteHooks` was a day old and only this library implements it. Code that mocks `SystemWebhooks`
  must add the method.
- `HookEventWorkflowRun` and `HookEventWorkflowJob`: GitHub, and Forgejo 17, where each stands for its seven per-status
  events. They return `ErrNotSupported` on older Forgejo servers, and on Bitbucket.
- `Event.WorkflowRun` and `Event.WorkflowJob` decode workflow deliveries from Forgejo 17 and GitHub. Both were tested
  against real deliveries. `Event.Name` is `workflow_run` or `workflow_job`, and on Forgejo `Event.Key` keeps the status.
- Webhook types on Forgejo: `HookType`, `CreateWebhookOptions.Type`, `Webhook.Type`, and `SlackHookSettings` for Slack
  and Mattermost. Telegram, Matrix, Packagist and SourceHut hooks can only be read: Forgejo's API drops their settings.
- `Webhook.HasAuthorizationHeader`. The value stays out of `Webhook`, as it is a credential.

### Changed
- `HookEventAll` on Forgejo follows the server's version: `workflow_*` from 17, without the deprecated `action_run_*`,
  which would notify a second time for each run; `action_run_*` below 17. A new "everything" webhook on 17 therefore
  delivers the new format. Existing webhooks do not change.
- **`Webhooks`, `UserWebhooks`, `SystemWebhooks` and `DefaultWebhooks`: `Edit` on Forgejo returns `ErrNotSupported` for
  `Secret`, and for an empty `AuthorizationHeader`.** Forgejo's API ignores both without an error, so a rotated secret
  was never applied.
- `Webhook.URL` and `ContentType` on Forgejo now come from the top-level fields, as `config` is deprecated.

### Fixed
- **`Edit` on Forgejo lost data.** Without events and a branch filter, Forgejo's `PATCH` resets them to `push` and none,
  on 15 and 17, so changing only `Active` or `URL` cut the webhook down to `push`. `Edit` now reads the webhook first
  and sends back what was not changed, which takes one more request.
- **`HookEventPullRequestReview` on Forgejo subscribed to nothing** and returned an error. The library sent the name of
  each verdict, which the server drops. It now sends `pull_request_review`. `pull_request_review_comment` is no longer
  folded into that constant, as it comes with `pull_request_comment`.
- The raw names `issues_only` and `pull_request_only` no longer report a dropped event.
- `Event.Repo` and `Event.Sender` are now set on Forgejo's `action_run_*` deliveries.

## [1.3.0] — 2026-11-05

Forgejo's user, system and default webhooks, what a pull request changes, comments on its lines, commit ranges and raw
diffs, a repository's collaborators, Forgejo's push mirrors, mirror sync and git hooks, GitHub's delivery history, more
typed webhook payloads, CI secrets and variables of an environment, GitHub organisation secrets limited to some
repositories, and fixes found by auditing pagination. Forgejo 15 is now the minimum.

### Added
- `EnvCI` (`CapCIEnvironments`) reaches the secrets and variables of a repository's environment through `Secrets()` and
  `Variables()`: GitHub's environments and Bitbucket's deployment variables. Forgejo has no environments and returns
  `ErrNotSupported`. `CIScope` gains an `Environment` field.
- **`CollaboratorInvitations`** (`CapCollaboratorInvitations`, GitHub only): `ListCollaboratorInvitations` shows who has
  been invited and has not accepted yet.
- **`OrgSecrets`** (`CapOrgSecrets`, GitHub only): `GetOrgSecretAccess`, `SetOrgSecret` and `SetOrgSecretRepos` limit an
  organisation secret to the private or selected repositories.
- **`GitHooks`** (`CapGitHooks`, Forgejo only): `ListGitHooks`, `GetGitHook`, `SetGitHook` and `DeleteGitHook`. Forgejo
  refuses them unless the instance sets `DISABLE_GIT_HOOKS=false`.
- **`HookDeliveries`** (`CapHookDeliveries`, GitHub only): `ListHookDeliveries`, `GetHookDelivery` and `RedeliverHook`.
  The APIs of Forgejo and Bitbucket have no delivery history.
- `HookEventAll` subscribes a webhook to everything: GitHub's wildcard, or on Forgejo and Bitbucket the events the library
  knows. `InsecureSSL` on webhooks skips the certificate check, on GitHub and Bitbucket.
- Typed webhook payloads: `Event.Issues`, `IssueComment`, `Review`, `ReviewComment`, `Create` and `Delete`.
- **`Collaborators`** (`CapCollaborators`, all backends): `ListCollaborators`, `GetCollaborator`, `AddCollaborator` and
  `RemoveCollaborator`. On GitHub, `AddCollaborator` reports when it sent an invitation, and `RemoveCollaborator` also
  withdraws one. Permissions a forge would misread are refused: Forgejo stores write for any value it does not know.
- **`Mirrors`** (`CapMirrors`, Forgejo only): `ListPushMirrors`, `GetPushMirror`, `CreatePushMirror`, `DeletePushMirror`,
  `SyncPushMirrors` and `SyncMirror`. Creating a pull mirror takes a migration, and is not included.
- **`PRContents`** (`CapPRContents`, all backends): `PullRequestDiff` (diff or patch, streamed), `PullRequestFiles` and
  `PullRequestCommits`, newest first.
- **`ReviewComments`** (`CapReviewComments`, all backends): `ListReviewComments`, `CreateReviewComment` and
  `DeleteReviewComment`, on a line of the new or the old version of a file.
- **`CommitDiffs`** (`CapCommitDiffs`, all backends): `CommitDiff` streams a commit's diff or patch.
- `ListCommitsOptions.Exclude`, like `git log A..B`, on all backends. `ListCommitsOptions.Author`, on GitHub; the other
  backends return `ErrNotSupported`.
- **`UserHooks`** (`CapUserWebhooks`, Forgejo only): `UserWebhooks()` lists, gets, creates, edits and deletes the
  authenticated user's own webhooks.
- **`SiteHooks`** (`CapSystemWebhooks`, Forgejo only, site admins):
  - `SystemWebhooks()`: list, get, edit, delete. There is no create, as Forgejo's API only offers it through an
    undocumented flag: they are created in the web interface.
  - `DefaultWebhooks()`: create, get, edit, delete. These are copied into each new repository, and there is no list,
    because Forgejo's API has none.
  - Each only reaches its own kind: an ID of the other gives HTTP 404, changing nothing. Forgejo's API does not tell
    the kinds apart, so the library checks by listing the system webhooks first.
- Unit tests for what only the live tests reached: Bitbucket refs, pull requests, repositories and protection; GitHub
  and Forgejo labels and milestones, GitHub issue edits; `internal/refusal`. Coverage is now about 88% for Bitbucket,
  87% for GitHub and 90% for Forgejo.
- Issue calls that take label names on Forgejo (`Create`, `Edit`, `AddLabels`, `RemoveLabel`) now also find the
  organisation's labels. A repository label wins over an organisation label of the same name.

### Changed
- Forgejo's review deliveries are now reported with the `Event.Name` `pull_request_review` and the `Action` `submitted`.
  Forgejo's own name (`pull_request_approved`, `pull_request_rejected`, `pull_request_comment`) moved to `Event.Key`.
- Bitbucket's `pullrequest:comment_created`, `_updated` and `_deleted` deliveries are now reported with the `Event.Name`
  `issue_comment`, or `pull_request_review_comment` on a line. The key stays in `Event.Key`.
- Forgejo 15.0.0 or later is now required: `forgejo.New` refuses older servers (`MinimumVersion` was 11.0.10).
- `CapSystemWebhooks` and `CapMirrors` are no longer reserved: the Forgejo client now announces them. `CapMigrate` still
  is.

### Fixed
- `Webhooks().List` on Bitbucket silently dropped webhooks past the first 10: that endpoint pages with a token, which
  `NextPage` cannot carry, and ignores `page=`. It now returns them all at once. Every other Bitbucket list now reads on
  when it meets such a token, instead of stopping.
- On Forgejo, `PerPage` without `Page` was ignored by some lists, which returned more items than asked. `/push_mirrors`
  also linked a next page, repeating items. The library now always sends a page with a limit.
- `HookEventAll` on Forgejo reported an error on versions without one of its events, such as `action_run_recover` on 17.
- `CreatePushMirror` explains a refused remote, where Forgejo 17 says "Unknown error" for a plain http address.
- `Secrets().Set` on a GitHub organisation made the secret available to all repositories every time, whatever its
  visibility was: changing a limited secret's value widened its access. It now keeps the visibility and repositories.
- `Webhooks().Edit` on Bitbucket keeps a webhook's "skip certificate verification" setting, which it used to reset.
- `gol.All` and `Page.NextPage` stopped after the first page on most Forgejo lists, silently dropping the rest: labels,
  milestones, webhooks, organisation members, teams, team members and repositories, secrets, variables, SSH
  keys, tokens and reviews. These send `X-Total-Count` but no `Link` header, so `NextPage` now falls back on the
  count, for every Forgejo list.
- Label names beyond the first page of a repository's labels were reported as not found by the calls that take names.
- `Merge` on Forgejo failed with HTTP 405 "Please try again later" while the server was still checking the pull
  request. It is now retried, under `RetryPolicy`.
- Forgejo errors whose JSON is followed by other content, such as an HTML page, lost their message.
- On Bitbucket, internal listings that read every page stopped early on an endpoint with page tokens instead of page
  numbers. They now follow the next link.

## [1.2.0] — 2026-11-05

Organisation avatars, and two fixes where `Account` promised what Forgejo does not do. Both were found by gitclt
on a real server: the library's fake servers answered the way it expected.

### Added
- **`OrgAvatars`** (`CapOrgAvatars`, Forgejo only): `SetOrgAvatar` and `DeleteOrgAvatar`, an optional interface
  found with `client.(gol.OrgAvatars)`. Images follow the same rules as `Profile().SetAvatar`.

### Changed
- Go 1.27.3 or later is now required.
- `SSHKeys().Add` on Forgejo refuses `AddSSHKeyOptions.ReadOnly` with `ErrNotSupported`, before any request. Code
  that sets it now gets an error instead of a key with more access than it asked for. All three backends now refuse
  the option.

### Fixed
- **Security-relevant:** `AddSSHKeyOptions.ReadOnly` had no effect on Forgejo. Its API accepts `read_only` on user
  keys, but stores a read-write key and reports no such field (checked on 11.0.16 and 15.0.11). Keys added with it
  through 1.1.0 are read-write: check them, and delete any that should not have write access.
- `SSHKey.ReadOnly` is documented as GitHub only, and is never set on Forgejo.
- `Emails().Add` on Forgejo and GitHub returned every address on the account: it now returns only the ones added.
  Its documentation no longer says they are unverified: without required confirmation, Forgejo reports them verified.

## [1.1.0] — 2026-10-30

Three new optional interfaces (`Account`, `CI`, `History`), transport hardening and Forgejo two-factor
authentication.
Nothing existing was removed or changed incompatibly: new areas are optional interfaces found by type
assertion, following the API-stability rules now in the README.

### Added
- **`Account`** (`CapAccount`, all three backends): the authenticated user's own account.
  - `Emails()`: list, add, delete. Bitbucket only lists.
  - `SSHKeys()`: list, get, add (`AddSSHKeyOptions`; read-only keys on Forgejo), delete.
  - `GPGKeys()`: list, get, add, delete. On Forgejo also `VerificationToken` and `Verify`, and adding a key
    together with a signature of the token.
  - `Profile()`: `Edit(EditProfileOptions)` on Forgejo and GitHub (`EditSettings` for Forgejo's
    preferences); `SetAvatar`/`DeleteAvatar` on Forgejo (`MaxAvatarSize`, 10 MiB). Bitbucket has no API.
  - `Tokens()` (`CapTokens`, Forgejo only): `List`, `Create` (`CreateTokenOptions`: scopes, optional
    repository restriction), `Delete`, `DeleteByName`. The secret is in `AccessToken.Secret`, returned once
    by `Create` and never serialised. Creating and deleting need a password.
- **`CI`** (`CapSecrets`, all three backends): secrets (`Secrets`: list, set, delete; write-only values)
  and variables (`Variables`: list, get, set, delete) for Forgejo Actions, GitHub Actions and Bitbucket
  Pipelines, at repository (`RepoCI`), organisation or workspace (`OrgCI`) and user (`UserCI`, Forgejo
  only) level. GitHub secrets are encrypted with the scope's public key (libsodium sealed box); Bitbucket
  secrets are secured Pipelines variables.
- **`History`** (`CapCommits`, all three backends): commit history read from the server, like `git log`.
  `Commits().List(owner, repo, ListCommitsOptions)` lists the commits reachable from a ref (default branch
  when empty), newest first, optionally limited to a path (and on GitHub to a date range).
  `Commits().Get(owner, repo, ref)` returns one commit, by SHA, branch or tag, with its changed files
  (`ChangedFile`: path, previous path, `FileStatus`, line counts) and totals (`CommitStats`). Forgejo reports
  no per-file line counts and no renames.
- **Retries** in all three backends: `WithRetry(RetryPolicy)`, `DefaultRetryPolicy` (3 attempts,
  server-requested waits up to 30 s, on by default), `NoRetry`.
  - Requests the server did not process are retried whatever their method: HTTP 429, rate limits, and
    GitHub's 409 "conflicting repository operation is still in progress".
  - HTTP 502/503/504 and dropped connections are retried only for GET, HEAD, PUT and DELETE.
  - Refused connections, timeouts and streamed uploads are never retried.
- `APIError.RetryAt`: when the server allows the request again (`Retry-After`, rate-limit reset).
- **Recognised refusals**, matched with `errors.Is` through the new `APIError.Err` field and
  `APIError.Unwrap`. The `*APIError` keeps the server's status and message.
  - `ErrOwnsRepos`, `ErrOrgMember`, `ErrLastOwner`: Forgejo user and organisation administration.
  - `ErrBranchProtected`: all three forges.
  - `ErrInvalidOTP`: Forgejo 2FA.
  - `ErrGPGSignature`: Forgejo GPG keys.
  - `ErrPasswordRequired`: Forgejo token management.
- Forgejo: `BasicAuthOTP(user, password, code)` and `BasicAuthOTPFunc(user, password, func() string)` for
  accounts with two-factor authentication (`X-FORGEJO-OTP`). A code is accepted for 30 to 60 seconds.
- Forgejo: `WithSudo(login)`, for site administrators acting as another user.
- Capability bits `CapAccount`, `CapTokens`, `CapSecrets` and `CapCommits`, appended after `CapDirectory`; existing
  values are unchanged and pinned by a test.
- New dependency `golang.org/x/crypto` (v0.58.0), used by the GitHub backend only, for the sealed box.

### Changed
- Transient failures that used to be returned at once are now retried. Pass
  `WithRetry(gitoperationslib.NoRetry)` for the previous behaviour.
- Bitbucket: `Directory().Me()` now reads GPG keys (`/users/{uuid}/gpg-keys`): `Missing` no longer lists
  "GPG keys". `SSHKey.ID` is derived from the key's UUID (it was 0).
- Forgejo: `GPGKeys().Verify` of a key whose ID starts with 0 fails on the server (HTTP 500, a Forgejo
  bug); the error matches `ErrNotSupported`, and the key can be added again with the signature instead.
- Contract and admin integration tests require the recognised refusals instead of any error, and cover
  `Account`, `CI` and `History` on all three forges.

## [1.0.0] — 2026-10-29

First stable release. From here on, the public API (types, interfaces, error types and JSON key names)
follows Semantic Versioning.

### Fixed
- Forgejo: a pull request whose head branch was deleted (after a merge with `DeleteBranch`, typically)
  reported `refs/pull/N/head` as `Head.Ref`. The branch name is now read from Forgejo's `label`, both by
  the `PullRequests` calls and by `webhook` pull request deliveries.

## [0.4.0] — 2026-10-27

### Added
- `Branch.LastCommit` (`CommitSummary`: message, author name, author date). `Get` always fills
  it; `List` does on Forgejo and Bitbucket, not on GitHub (nil there), whose branch list only
  carries the commit SHA.
- `Branches.Compare(ctx, owner, repo, base, head)`, returning a `Comparison`: the commits of
  head missing from base (`Ahead`), and the reverse (`Behind`). One request on GitHub, two on
  Forgejo, and on Bitbucket (no comparison endpoint) a count of the commits, 100 per request.
- Contract test: `LastCommit` from `Get`, and `Compare` between two branches at the same commit.

### Changed
- **Breaking** for code implementing `Branches` (fakes, wrappers): the interface gains `Compare`.

## [0.3.0] — 2026-10-23

JSON support. The JSON key names are now part of the public API.

### Added
- JSON tags on every public struct: results, option structs, error types (`APIError`,
  `NotSupportedError`, `UnsupportedVersionError`), `webhook` events and `forgejo.Version`.
  - Keys are camelCase; acronyms are written as words (`htmlUrl`, `sshUrl`, `keyId`).
  - Common fields are always present; fields some backends don't report, nil pointers and zero
    times are omitted (`omitzero`).
  - Option structs omit everything except their required fields; nil `Edit…Options` fields stay
    nil when unmarshalled.
  - Secrets are never serialised: `Password` (user options), `Secret` and `AuthorizationHeader`
    (webhook options) are tagged `json:"-"`.
- `PartError` and a `Missing []PartError` field on `UserInfo`, `OrgInfo`, `TeamInfo` and
  `CurrentUser`: the refused parts as data (part, HTTP status, message), since `Incomplete` (an
  `error`) is not serialised.
- A test that fails when an exported field lacks a camelCase JSON tag.
- `__docs/USAGE.md` §3.7 (JSON).

### Changed
- `webhook.Event.Payload` is now `json.RawMessage` instead of `[]byte`, so it marshals as JSON
  rather than base64. It has the same underlying type, so `json.Unmarshal(ev.Payload, …)` and
  assignments to `[]byte` still compile.
- `Incomplete` is tagged `json:"-"`.

## [0.2.1] — 2026-10-23

### Added
- `Directory.Me`: the authenticated user, from the credentials, with everything the token may
  read: private profile fields, e-mail addresses, organisations, teams, repositories, SSH
  keys (Forgejo also settings; GitHub also plan and private counts). Refused or unsupported parts
  are reported in `Incomplete`. Implemented on Forgejo, GitHub and Bitbucket (workspaces as
  organisations; no teams or GPG keys).
- New types `CurrentUser`, `UserSettings`, `Email`, `SSHKey`, `GPGKey`, `GPGKeyEmail`.
- `contracttest.Directory` checks `Me` on every backend.

### Changed
- `gitoperationslib.Directory` gains `Me`: implementations outside this module must add it.

## [0.2.0] — 2026-10-22

### Added
- `Directory` (`Client.Directory()`, `CapDirectory`): reads users, organisations and teams with
  any token (the server filters what the caller may see), on Forgejo and GitHub; Bitbucket returns
  `ErrNotSupported`. `User`, `UserInfo`, `Org`, `ListOrgs`, `ListAllOrgs`, `OrgInfo`, `OrgMembers`,
  `ListTeams`, `Team`, `TeamInfo`, `TeamMembers`, `TeamRepos`. Teams are addressed by organisation
  and name (GitHub: slug).
- `UserInfo`, `OrgInfo`, `TeamInfo`: an object with its lists (organisations and repositories of
  a user; teams, members and repositories of an organisation; members and repositories of a
  team). Lists the caller may not read are left empty and reported in `Incomplete`.
- New fields: `User.AvatarURL`, `Company` (GitHub), `Pronouns`, `Language`, `StarredRepos`
  (Forgejo), `Followers`, `Following`; `Org.AvatarURL`, `Org.HTMLURL` (GitHub); `Team.Slug`.
- Contract check `contracttest.Directory` (read-only).

### Changed
- `gitoperationslib.Client` gains `Directory()`: implementations outside this module must add it.
- Package documentation updated for Bitbucket (`doc.go`, `webhook`); `webhook.ErrUnknownForge`'s
  message now names Bitbucket.

### Documentation
- README rewritten (architecture, design choices, backend differences); `__docs/USAGE.md` added,
  covering every package, interface and option.

## [0.1.7] — 2026-10-21
Nothing changed, functionally, really.

## Added
A LICENSE file, so documentation gets properly shown on https://pkg.go.dev/
## [0.1.6] — 2026-10-20

### Added
- `bitbucket.APIToken(email, token)`: Atlassian API tokens (ATATT...), sent through basic
  authentication with the Atlassian account e-mail, the only way Bitbucket accepts them.
- Bitbucket integration tests: `GITOPSLIB_BITBUCKET_EMAIL`, to run them with an API token.

### Fixed
- `bitbucket.Token` documentation: it only covers workspace/project/repository access tokens
  (ATCTT...); Bitbucket refuses Atlassian API tokens as bearer tokens (HTTP 401).

## [0.1.5] — 2026-10-20

### Added
- **Bitbucket Cloud backend** (`bitbucket` package), on a hand-written REST 2.0 transport
  (`internal/bitbucketapi`). Repository owners are workspaces. Authentication with
  `bitbucket.Token` (bearer: Atlassian API tokens, workspace/project/repository access
  tokens) or `bitbucket.BasicAuth`; options `WithHTTPClient`, `WithUserAgent`, `WithBaseURL`.
  - Repositories: get, list, create (`AutoInit` commits a `README.md`), edit (renaming changes
    the slug), delete.
  - Branches and tags: list (default branch flagged), get, create from a branch, tag or
    commit, delete. Bitbucket always annotates tags; its generated message reads back as empty.
  - Branch protection through branch restrictions: required approvals, stale-approval reset
    (Premium plans), deletion and force-push blocks, push and merge allowlists.
  - Pull requests: list, get, create (drafts included), edit, decline, merge (merge commit,
    squash, rebase, rebase-merge, fast-forward-only; `HeadSHA` checked before merging;
    source-branch deletion), reviewer requests, approvals, change requests, comments, reviews
    listed from participants.
  - Repository and workspace webhooks: list, get, create, edit, delete, with event names
    translated to Bitbucket's keys and numeric IDs derived from Bitbucket's UUIDs.
  - Everything Bitbucket Cloud lacks returns `ErrNotSupported`: releases, issues, labels and
    milestones (the issue tracker was retired by Atlassian), tag protection, branch rename,
    reopening declined pull requests, auto-merge, team reviewers, named status checks,
    webhook pings, form-encoded payloads.
- `gitoperationslib.BackendBitbucket`.
- `webhook` package: Bitbucket deliveries are recognised (`X-Event-Key`), verified
  (`X-Hub-Signature: sha256=…`) and decoded: `Push()` and `PullRequest()` read Bitbucket's
  payloads. Event keys are mapped to the common names (`repo:push` → `push`,
  `pullrequest:…` → `pull_request` / `pull_request_review` with an action); the new
  `Event.Key` field keeps the raw key. Built from Bitbucket's documentation and tested with
  synthetic fixtures.
- `__docs/support_grid.md`: support level of every feature, per backend.
- `__docs/CHANGELOG.md` (this file).
- `__docs/forgejo-swagger-15.0.2.json`: Forgejo 15.0.2's API definition, for reference.
- `_dontexec` marker: the module is a library, not an executable.
- `__docs/TODO.md`: next priorities (Forgejo access tokens, then GitHub App authentication),
  CI kept on hold, and an assessment of GitLab and Bitbucket as further backends.

### Changed
- `webhook.Verify` also accepts Bitbucket's `X-Hub-Signature` when it carries a `sha256=`
  signature (GitHub's legacy SHA-1 header is still ignored).
- Contract checks (`internal/contracttest`) now hold each backend to its documented gaps: an
  area a backend lacks must return `ErrNotSupported` instead of being skipped. Verified
  against Forgejo, github.com and Bitbucket Cloud.
- README: Bitbucket client setup, receiving Bitbucket webhooks, Bitbucket test variables;
  the feature and backend-difference grids moved to `__docs/support_grid.md`.
- Repository history rewritten (commit messages only): every commit after the initial one
  has a new hash, and `v0.1.0` was re-tagged on the rewritten commit with identical file
  contents (Go module checksums are unchanged).

## [0.1.0] — 2026-10-17

### Added
- Core API in the root package: the `Client` interface and its areas (`Repos`, `Branches`,
  `Tags`, `BranchProtections`, `TagProtections`, `Releases`, `PullRequests`, `Issues`,
  `Labels`, `Milestones`, `Webhooks`), the optional `Admin` and `ExternalAssets` interfaces,
  capability flags, `ErrNotSupported`, `APIError` with status helpers,
  `UnsupportedVersionError`, `ErrOwnerTeam`, `ListOptions` / `Page[T]` and the `All` iterator,
  and `Ptr`.
- **Forgejo backend** (`forgejo` package), on a hand-written transport
  (`internal/forgejoapi`): server version check (minimum 11.0.10), token and basic
  authentication.
  - Repositories, branches, tags, branch and tag protection rules.
  - Releases and assets: streamed multipart uploads with exact `Content-Length`, streamed
    downloads (credentials only sent to the instance), external assets.
  - Pull requests (drafts through the `WIP:` title prefix, all six merge methods, auto-merge
    and its cancellation), review requests and reviews.
  - Issues, labels, milestones and comments.
  - Repository and organisation webhooks, with dropped (unknown) events reported.
  - Administration: users (including rename and purge), organisations and members, teams with
    per-unit access (`TeamAccess`) and a protected owner team.
- **GitHub backend** (`github` package), on `google/go-github` v93 plus a minimal GraphQL
  client: repositories, branches, tags, rulesets for branch and tag protection, releases and
  streamed assets, pull requests (draft changes and auto-merge through GraphQL), reviews,
  issues (deletion through GraphQL), labels, milestones, comments, repository and
  organisation webhooks; GitHub Enterprise Server through `WithEnterpriseURL`.
- `webhook` package: HMAC-SHA256 signature verification, forge detection (Forgejo checked
  before GitHub), form-encoded payloads, `http.Handler`, typed push, pull request and release
  payloads; fixtures captured from real Forgejo and GitHub deliveries.
- Shared contract checks (`internal/contracttest`) run against each backend by its
  integration tests (`-tags integration`).
- `__scripts/forgejo-testenv.sh`: disposable local Forgejo in Docker for the admin and webhook
  delivery tests (admin tests refuse non-loopback URLs).
- README with worked examples for every area; `__docs/TODO.md`.
