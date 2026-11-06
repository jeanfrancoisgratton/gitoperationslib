# TODO

Deferred items and open questions. Remove entries when done; note the increment that resolved them.

___
## Miscellaneous

1.0.0 (2026-10-29) was meant to be feature-complete. 1.1.0 (2026-10-30) reopened it for the gaps found in gitclt: `Account`, `CI`, `History` (commit log), Forgejo 2FA and Sudo, transport hardening (see __docs/DESIGN-1.1.0.md). The points weighed before 1.0.0:

- [x] **1 →** API stability: new areas land as optional interfaces found by type assertion, never as methods on existing interfaces (README, "Installation"). Decided for 1.1.0, see __docs/DESIGN-1.1.0.md.
- [ ] **2 →** Areas no consumer exercises yet (gitclt uses repositories, branches, tags, releases, pull requests, `Directory` and `Admin`): webhooks, issues/labels/milestones, branch and tag protection, and since 1.1.0 `Account`, `CI` and `History` (covered by the live contract tests on all three forges, but not by a real tool). Mark them experimental in the documentation, or exercise them first.
- [x] **3 →** Transport hardening: retries/backoff on transient errors, GitHub rate-limit handling (see the Transport sections), and typed errors for the HTTP 500 refusals (Forgejo "org still owns repositories" and "last member of owner team", Bitbucket protected-branch deletion). 1.1.0 increment 1.
- [x] **4 →** Unit-test coverage: GitHub backend ~75%, Bitbucket ~65% (was ~53% before 1.1.0), Forgejo ~85%.

## Candidates for later minor releases

From the 1.1.0 review of gitclt; nothing decided. 1.2.0 holds only the fixes and `OrgAvatars` from the gitclt 1.4.0 work.
- [x] **5 →** Repository settings: `Collaborators` (all backends) and `Mirrors` (push mirrors and mirror sync, Forgejo), 1.3.0. Deploy keys were dropped from this item, and `CapMigrate` stays reserved.
- [x] **6 →** CI: `EnvCI` (GitHub environment secrets and variables, Bitbucket deployment variables) and `OrgSecrets` (GitHub organisation secrets limited to selected repositories), 1.3.0. Forgejo needs nothing more: its API has no environments or limited secrets, and its repository, organisation and user secrets already exist (a user's cannot be listed).
- [ ] **7 →** Commit statuses (`POST /statuses/{sha}`): would let the live tests exercise auto-merge's success path.
- [x] **8 →** Pull request extras: `PRContents` (diff/patch, changed files, commits) and `ReviewComments` (line comments: list, create, delete; not edit), 1.3.0.
- [x] **9 →** Commit history: `ListCommitsOptions.Exclude` (`git log A..B`; GitHub through a comparison), `Author` (GitHub only), and `CommitDiffs` (diffs/patches of a commit), 1.3.0.

## Access tokens (Forgejo)

- [x] **10 →** **Forgejo access tokens**: `Account.Tokens()` (list, create with scopes and repository restriction, delete by ID or name), 1.1.0 increment 5. Findings on 15.0.11: listing accepts tokens (and a site admin lists anyone's), creating and deleting need basic auth (401 "auth method not allowed" → `ErrPasswordRequired`, even for an admin with Sudo); a numeric name is taken for an ID on delete, so `DeleteByName` lists first; 2FA accounts work with `BasicAuthOTP`.

## Deferred: GitHub App authentication

- [ ] **11 →** Scoped out for later: the main server is Forgejo (git.famillegratton.net), and nothing needs it yet; pick it up when a real consumer (bot, CI job) does. A client constructor taking an app ID and private key (PKCS#1 and PKCS#8), minting short-lived installation tokens (JWT signed with the standard library, no new dependency) and refreshing them automatically (concurrency-safe cache), for both go-github and the GraphQL client; revocation via `DELETE /installation/token`. Testing needs a registered, installed GitHub App. Possibly also org-level listing/revocation of fine-grained PATs (GitHub App only) and the leaked-credential revocation endpoint (verify it exists).

## Transport (Forgejo)

- [x] **12 →** Multipart upload for release assets (streamed, exact Content-Length). Increment 6.
- [x] **13 →** `Sudo` header option: admin acting as another user (`forgejo.WithSudo`). 1.1.0 increment 2.
- [x] **14 →** Basic auth with TOTP (`X-FORGEJO-OTP`) for accounts with 2FA: `BasicAuthOTP` (fixed code) and `BasicAuthOTPFunc` (code per request); `ErrInvalidOTP` when the code is missing or invalid. A code is valid from its 30-second period through the next one (Forgejo accepts ±1 period). 1.1.0 increment 2.
- [x] ~~**15 →** `__scripts/forgejo-testenv.sh` could enrol a 2FA user for `TestIntegrationOTP` (web-only flow: log in, read the secret on `/user/settings/security/two_factor/enroll`, post a code). Done by hand with a Python script on 2026-10-30.~~
- [x] **16 →** Retries / backoff on transient errors (5xx, connection reset). Forgejo sends no rate-limit headers. 1.1.0 increment 1 (`internal/retry`, shared by all backends).
- [ ] **17 →** `/api/forgejo/v1` namespace client, for Forgejo-only endpoints. Its swagger was not found at `/api/forgejo/v1/swagger.json` on 15.0.2; locate it.

## Transport (GitHub)

- [x] **18 →** Rate-limit handling: short waits are retried; longer ones return `APIError.RetryAt`. 1.1.0 increment 1. go-github also refuses requests on its own while it knows the limit is exhausted (no request sent); those errors carry `RetryAt` too.
- [x] **19 →** GitHub returns 409 "conflicting repository operation is still in progress" for edits/deletes right after a create with `AutoInit`: the library retries it (1.1.0 increment 1). The contract tests' own `retryConflict` can go once a live GitHub run confirms the message matches.
- [ ] **20 →** Org-repo endpoints with an actual GitHub org are untested (the test account uses the personal namespace as "org"); unit tests cover the `/orgs/{org}/repos` path.
- [ ] **21 →** `DefaultBranch` on create returns `ErrNotSupported`; possible emulation (create with `AutoInit`, then rename branch) if needed.

- [ ] **22 →** Secondary rate limit on content creation: running the full GitHub integration suite several times in a row blocks repo creation for ~20 minutes. Consider fewer repos per run (e.g. one shared repo per suite).
- [ ] **23 →** GitHub list endpoints are eventually consistent (a just-created release may be missing from `List` for a second or two); contract tests poll.

## Releases

- [x] **24 →** Releases and assets on both backends, streamed upload/download, external assets (Forgejo), backend-specific options. Increment 6.
- [ ] **25 →** Asset `label` (GitHub) and editing an external asset's URL (Forgejo) are not exposed.
- [ ] **26 →** `List` filters (Forgejo `draft`, `pre-release`, `q`) are not exposed.
- [x] **27 →** Tags are kept when a release is deleted; delete them with `Tags().Delete`. Closed: intended behaviour, documented in USAGE, the support grid and `Releases.Delete`.
- [x] ~~**28 →** Undecided, much later: a `Tags().Release(tag, opts)` shortcut. `Releases().Create` with `TagName` already creates the tag if needed or releases an existing one.~~

## Pull requests

- [x] **29 →** Pull requests (list, get, create, edit, close/reopen, merge, is-merged), review requests and reviews on both backends. Increment 7.
- [x] **30 →** GitHub draft status changes and auto-merge via a minimal GraphQL client; `CancelAutoMerge` on both backends. Enhancement 1.
- [ ] **31 →** GitHub auto-merge success path is unit-tested only: it needs a required status check, i.e. branch protection (public repositories on free plans).
- [ ] **32 →** Forgejo drafts rely on the default `WORK_IN_PROGRESS_PREFIXES` (`WIP:`, `[WIP]`); a mismatch is reported, not worked around.
- [ ] **33 →** Not exposed yet: editing a line comment, update-branch, labels/milestones/assignees (wait for the issues area).
- [ ] **34 →** `IsMerged` reports false for a pull request that does not exist (both servers answer 404).

## Webhooks

- [x] **35 →** Repository and organisation webhooks on both backends; `webhook` sub-package for receiving (verification, forge detection, typed push/pull_request/release). Increment 8.
- [x] **36 →** User, system and default webhooks: `UserHooks`, `SiteHooks` (1.3.0). Checked on 15.0.11, with system hooks really created in the web interface.
- [x] **37 →** `SystemWebhooks.Create` uses the flag since 1.4.0. Forgejo: `POST /admin/hooks` makes a default webhook unless `config` holds the undocumented string `"is_system_webhook": "true"`, which makes a real system webhook (a top-level field is ignored; checked on 15.0.11 and 17.0.1, correcting an earlier note here). `GET /admin/hooks` only lists system hooks; `/admin/hooks/{id}` takes both, and its answers do not tell them apart, so `SystemWebhooks` and `DefaultWebhooks` check the kind by listing. `PATCH /admin/hooks/{id}` answers 500 on a repository hook's ID. Report 1 in `__docs/UPSTREAM-REPORTS.md`, as a record: not being filed.
- [x] **111 →** Forgejo's webhook API, on 15.0.11 and 17.0.1: a `PATCH` without events and a branch filter resets them to `push` and none, and it ignores a new secret, an empty `authorization_header`, and the `package`, `action_run_*` and `workflow_*` events (`Edit` refuses to change those, 1.5.1). A verdict's event name is dropped, `issues` is reported for `issues_only`, and Telegram, Matrix, Packagist and SourceHut lose their settings. The library copes (1.4.0). Reports 4 to 6 in `__docs/UPSTREAM-REPORTS.md`, as a record: not being filed.
- [x] **38 →** Audited all list endpoints on 15.0.11: most send X-Total-Count but no `Link` header, so `All` stopped after the first page. `forgejoapi.Response.Next` now falls back on the count for every list (1.3.0).
- [x] **39 →** Closed: it concerned Forgejo 11, and 15.0.0 is now the minimum (1.3.0).
- [x] **40 →** A merge sometimes got HTTP 405 "Please try again later" while the server checked the pull request. The library now retries it (1.3.0); `TestIntegrationPullRequests` passed five runs in a row on 15.0.11.
- [x] **41 →** Closed: these failures were Forgejo 11 differences, and 15.0.0 is now the minimum (1.3.0). The suite passes on 15.0.11.
- [x] **42 →** `contracttest.create` deletes the hooks Forgejo copies into a new repository (1.3.0). Checked with a default hook on a local listener: no delivery during the main contract tests. System and organisation webhooks would still fire.
- [x] **43 →** `GitHooks` (Forgejo), `HookDeliveries` (GitHub; the APIs of Forgejo and Bitbucket have no delivery history), `InsecureSSL` (GitHub, Bitbucket), `HookEventAll`, 1.3.0.
- [x] **44 →** `Event.Issues`, `IssueComment`, `Review`, `ReviewComment`, `Create` and `Delete`, 1.3.0. Forgejo's review events are `pull_request_approved`, `pull_request_rejected` and `pull_request_comment` (a review, not a conversation comment), checked on 15.0.11.

## Branches and tags

- [x] **45 →** Branches (list with default flag, get, create from branch/tag/commit, rename, delete) and tags (list, get, create lightweight or annotated, delete) on both backends. Enhancement 2a.
- [x] **46 →** Branch and tag protection: Forgejo protection rules, GitHub rulesets (shared subset + backend-specific options). GitHub tested on a short-lived public repository (free plans offer rulesets only there). Enhancement 2b.
- [ ] **47 →** GitHub rulesets: individual users cannot be exempted (teams only, organisations only); exempted teams bypass every rule of the ruleset, not just pushes.
- [ ] **48 →** GitHub: rulesets targeting several patterns, `~DEFAULT_BRANCH`, or set at organisation level are listed but cannot be addressed by pattern. Classic branch protection is not used.
- [ ] **49 →** Forgejo tag protection requires at least one allowed user or team.
- [ ] **50 →** Forgejo reports the commit message as `message` for lightweight tags; the library keeps it only for annotated tags (tag object ID differs from the commit).
- [ ] **51 →** GitHub `Tags.List` does not report annotations (one extra request per tag); use `Get`.
- [ ] **52 →** The integration tests' `CreateBranch` helpers could add their file commit through a future contents API.

## Issues

- [x] **53 →** Issues (list without pull requests, get, create, edit, close/reopen, delete), labels on issues and pull requests, comments, repository labels and milestones on both backends. Enhancement 2c.
- [x] **54 →** Organisation labels are now found by name on issues, with a second listing only for names missing from the repository (1.3.0). Resolving names still takes one listing per call: the issue endpoints need IDs, and labels are not cached because they can change. `Labels()` still manages repository labels only. Forgejo refuses an organisation label's ID when removing it from an issue (HTTP 422, 15.0.11) but takes its name, so `RemoveLabel` falls back on the name; a numeric organisation label name cannot be removed, since it could be taken for another label's ID.
- [x] **55 →** Forgejo does not paginate issue comments (`ListComments` returns them all). Closed: a server limitation the library already handles; the endpoint only offers `since` and `before`, which are not exposed.
- [ ] **56 →** Deferred (2026-11-05). Not exposed yet: reactions, timeline, attachments, due dates, pinned issues, issue types/fields (GitHub), state reasons (GitHub).
- [x] **57 →** Unit-test coverage of the GitHub backend dropped to ~76% (labels/milestones list/get were covered by the integration checks only). Closed: now ~86%, with unit tests for those paths.

## API surface

- [ ] **58 →** Optional interface for Forgejo's migration, which also creates pull mirrors (`CapMigrate` already declared). Mirrors and push mirrors: `Mirrors`, 1.3.0.
- [ ] **59 →** Forgejo-only create options (`trust_model`, `object_format`, `readme`) on `CreateRepoOptions`.
- [x] **60 →** Forgejo repo create/edit options only have `private`; `VisibilityInternal` returns `ErrNotSupported`. Responses carry a read-only `internal` flag, mapped to `VisibilityInternal`. Resolved in increment 3.
- [ ] **61 →** Cache user-vs-org lookups (`GET /orgs/{name}`), currently one extra request per `Create`/`List` with an owner.
- [ ] **62 →** Renaming a repo leaves a redirect on Forgejo; check whether `Get` on the old name should be documented.

## Admin (Forgejo)

- [x] **63 →** 5a Users: list, get, create, edit, rename, delete (purge). Increment 5a.
- [x] **64 →** 5b Orgs (get, list, list all, create incl. for another user, edit, rename, delete) and members (list, check, remove). Increment 5b.
- [x] **65 →** Forgejo returns HTTP 500 (not 4xx) for "org still owns repositories" and "last member of owner team": recognised as `ErrOwnsRepos` / `ErrLastOwner` (1.1.0 increment 1, messages checked on 15.0.11). Still worth reporting upstream.
- [x] **66 →** 5c Teams (list, get, get by name, create, edit, delete), members and repos. `TeamAccess` hides Forgejo's permission/units_map interplay; owner team protected by `ErrOwnerTeam`. Increment 5c.
- [ ] **67 →** Forgejo lets an admin delete an org's owner team (204) and rename it; the library refuses both. Consider reporting upstream.
- [ ] **68 →** Forgejo 15.0.11 bug: `POST /user/gpg_key_verify` fails with HTTP 500 "public gpg key does not exist [id: 0]" for every key whose ID starts with 0 (about 1 in 16; the signer's key ID seems to lose its leading zero). Adding the key with `armored_signature` works. The library maps it to `ErrNotSupported` with a hint (1.1.0 increment 4). Report upstream at codeberg.org/forgejo/forgejo.
- [ ] **69 →** Forgejo answers HTTP 500 for invalid avatar images (unknown format, side over 4096 px) and stores any theme name in `/user/settings`. Consider reporting upstream.
- [ ] **70 →** Forgejo accepts `read_only` on `POST /user/keys` (it is in the swagger) but stores `mode=2`, read-write, and reports no such field (15.0.11). The library refuses `ReadOnly` (1.2.0); `TestIntegrationAdminSSHKeyReadOnlyIgnored` fails if the response starts reporting it. Worth reporting upstream, as it is security-relevant.
- [ ] **71 →** Forgejo 15.0.x bug: `PATCH /admin/users/{u}` ignores `"visibility":"public"` (zero value of its enum). `Users.Edit` returns `ErrNotSupported` with the updated user when the server keeps the old value. Consider reporting upstream at codeberg.org/forgejo/forgejo.
- [x] **72 →** Interfaces are frozen since 1.0.0: adding a method to an existing interface (as `Admin` gained `Orgs()`/`Teams()`, or `Branches` gained `Compare` in 0.4.0) breaks external implementations such as mocks, so it needs a new major version. New behaviour goes through new optional interfaces, found by type assertion like `Admin` (decided for 1.1.0). At 2.0.0 they may be folded into `Client`.

## Open items from DESIGN-NOTES §8

- [x] **73 →** Forgejo draft PRs: a title prefix (`WIP:`/`[WIP]`), no API field; drafts cannot be merged (405). Increment 7.
- [x] **74 →** Forgejo `mergeable` is a plain bool, set immediately; GitHub's is nullable (`PullRequest.Mergeable *bool`). Increment 7.
- [x] **75 →** Forgejo pagination: `page`/`limit`, `Link` + `X-Total-Count` headers; defaults are instance settings (`/api/v1/settings/api`: 30 default, 50 max on git.famillegratton.net). Resolved in increment 2.
- [x] **76 →** Webhook signatures: both forges send `X-Hub-Signature-256: sha256=<hex HMAC-SHA256>`; Forgejo also `X-Forgejo-Signature` (bare hex) and GitHub-style event headers. Verified against real deliveries from both. Increment 8.
- [ ] **77 →** Forgejo asset upload limits and content-type handling.
- [ ] **78 →** Changes to Forgejo user/org/team endpoints in recent majors (quota APIs etc.).
- [x] **79 →** `go-github`: pinned at v93.0.0 (declares go 1.26.0). New majors change the import path; review periodically. Resolved in increment 4.
- [x] **80 →** Minimum Go version: 1.27.3. Minimum Forgejo version: 15.0.0.
- [x] ~~**81 →** Exact set of "Other" areas beyond the five priorities.~~

## Testing

- [x] **82 →** Integration tests: `-tags integration` + `GITOPSLIB_FORGEJO_{URL,TOKEN,ORG}` (token scopes: repository, organization, user, issue — read and write); repos prefixed `gitopslib-it-`, cleanup deletes only those. Added in increment 3.
- [x] **83 →** Shared contract checks in `internal/contracttest`, run by both backends' integration tests. Added in increment 4.
- [x] **84 →** GitHub user-namespace: covered by setting `GITOPSLIB_GITHUB_ORG` to the account's own login. Increment 4.
- [x] **85 →** Disposable Forgejo for admin tests: `__scripts/forgejo-testenv.sh start|stop` (Docker, loopback only, writes `.env.testenv`). Admin tests refuse non-loopback URLs. Increment 5a.
- [ ] **86 →** CI — **on hold until really needed** (no Forgejo/GitHub Actions for now). When picked up: (1) checks: gofmt, go vet (with and without the integration tag), go test -race; (2) Forgejo integration against the disposable container only, never production (testenv script would also create a CI org and write `GITOPSLIB_FORGEJO_*`); (3) GitHub integration on manual trigger only (token as a CI secret; rate limits; public repo for protection checks). Platform undecided (Jenkins, Forgejo Actions, GitHub Actions).

## Bitbucket Cloud

- [x] **87 →** Bitbucket Cloud backend (`bitbucket/`, REST 2.0, hand-written transport in `internal/bitbucketapi`): repositories, branches, tags, branch protection (branch restrictions), pull requests and reviews, webhooks; `webhook` package receives Bitbucket deliveries. Contract checks pass on the user's workspace. Users/workspace administration deliberately not implemented.
- [ ] **88 →** Not available on Bitbucket Cloud (`ErrNotSupported`): releases (only a repository-wide Downloads area: a Bitbucket-only `Downloads` interface was declined for now), issues/labels/milestones (tracker retired, CHANGE-3071, HTTP 410), tag protection, branch rename, reopening declined pull requests, auto-merge, team reviewers, named status checks (only "N passing builds"), webhook ping, form payloads.
- [x] **89 →** Bitbucket answers **HTTP 500 "Something went wrong"** when deleting a branch protected by a restriction (it is a refusal: the same delete succeeds once the restriction is gone). Recognised as `ErrBranchProtected` after checking the delete restrictions (1.1.0 increment 1); confirmed live 2026-10-30.
- [ ] **90 →** Receiving Bitbucket webhooks follows the documentation (synthetic fixtures); verify against a live delivery once a public endpoint is available.
- [ ] **91 →** `ApplyToAdmins` has no Bitbucket equivalent (ignored on write, reported true). Resetting stale approvals and enforced merge checks need a Premium plan (the server error is passed through).
- [ ] **92 →** Users have no public login: nicknames are reported; nickname → UUID lookups use workspace members only.
- [ ] **93 →**  IDs are derived from Bitbucket's UUIDs (first 15 hex digits); `Get`/`Edit`/`Delete` list the hooks to find them.
- [ ] **94 →** `Repos().List("")` lists each workspace (Bitbucket retired `/repositories?role=member`); with several workspaces everything is returned as one page.
- [ ] **95 →** Unit-test coverage of `bitbucket/` is ~53% (read paths and the ErrNotSupported stubs are covered by the integration checks only).
- [x] **96 →** Audited Bitbucket's lists on 2026-11-05. Webhooks page with `next=<uuid>` and ignore `page=`, which silently dropped all but the first 10: `Webhooks().List` now returns them all at once. A pull request's commits use `page=67Fg`. Branches, tags, pull requests, repositories, diffstat and commits use numbers. Collaborators, SSH and GPG keys could not be measured with one entry, so `bitbucketapi.ListPaged`, used by all public lists, reads on when it meets a token instead of stopping.
- [x] **97 →** Forgejo bug, on 15.0.11, 16.0.7 and 17.0.1: a list requested without `page` ignores `limit` on `/push_mirrors`, labels, milestones, webhooks and deploy keys. `/push_mirrors` also links a next page, which repeats items. Fixed on our side: `ListQuery` always sends a page with a limit. Reports 2 and 3 in `__docs/UPSTREAM-REPORTS.md` (also the `%!s(<nil>)` messages), as a record: not being filed.
- [x] **99 →** Finished for 1.5.0, on 2026-11-05: `TestIntegrationHookDeliveries`, `TestIntegrationWebhooks` and `TestIntegrationCI` pass on the `github` package, and `Review`, `ReviewComment`, `Create` and `Delete` are tested against deliveries captured from GitHub's history. Comments on a removed or outdated line still follow its documentation. Still untested, needing what the test account lacks: `AddCollaborator`, `RemoveCollaborator` and invitations with an entry need a second account (`GITOPSLIB_GITHUB_COLLABORATOR`), as that sends a real invitation; `OrgSecrets` and the visibility fix in `Secrets().Set` need a real organisation, so they only have unit tests, based on GitHub's documentation.
- [x] **98 →** GitHub's pending invitations: `CollaboratorInvitations`, 1.3.0, read live on a real repository (none pending). The add, change and remove path is part of item 99.

