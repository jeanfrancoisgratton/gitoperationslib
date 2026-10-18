# TODO

Deferred items and open questions. Remove entries when done; note the increment that resolved them.

## Next priority

- [ ] **Forgejo access tokens** (`Tokens` interface): list, create (name, scopes such as `write:repository`, optional repository restriction), revoke by name or ID, via `/users/{username}/tokens`. The secret is returned only once, by create. Forgejo is expected to require username/password authentication (a token cannot mint tokens): confirm on the disposable instance, and whether a site admin can manage other users' tokens. GitHub: `ErrNotSupported` (personal access tokens cannot be created or revoked through the API).
- [ ] **GitHub App authentication** (after Forgejo tokens): a client constructor taking an app ID and private key, minting short-lived installation tokens (JWT signed with the standard library, no new dependency) and refreshing them automatically; revocation via `DELETE /installation/token`. Possibly also org-level listing/revocation of fine-grained PATs (GitHub App only) and the leaked-credential revocation endpoint (verify it exists).

## Transport (Forgejo)

- [x] Multipart upload for release assets (streamed, exact Content-Length). Increment 6.
- [ ] `Sudo` header option: admin acting as another user.
- [ ] Basic auth with TOTP (`X-FORGEJO-OTP`) for accounts with 2FA.
- [ ] Retries / backoff on transient errors (5xx, connection reset). Forgejo sends no rate-limit headers.
- [ ] `/api/forgejo/v1` namespace client, for Forgejo-only endpoints. Its swagger was not found at `/api/forgejo/v1/swagger.json` on 15.0.2; locate it.

## Transport (GitHub)

- [ ] Rate-limit handling: go-github returns `RateLimitError` (converted to `APIError` with reset time); consider waiting/retrying or exposing the reset time as a field.
- [ ] GitHub returns 409 "conflicting repository operation is still in progress" for edits/deletes right after a create with `AutoInit`. Contract tests retry; decide whether the library should retry itself.
- [ ] Org-repo endpoints with an actual GitHub org are untested (the test account uses the personal namespace as "org"); unit tests cover the `/orgs/{org}/repos` path.
- [ ] `DefaultBranch` on create returns `ErrNotSupported`; possible emulation (create with `AutoInit`, then rename branch) if needed.

- [ ] Secondary rate limit on content creation: running the full GitHub integration suite several times in a row blocks repo creation for ~20 minutes. Consider fewer repos per run (e.g. one shared repo per suite).
- [ ] GitHub list endpoints are eventually consistent (a just-created release may be missing from `List` for a second or two); contract tests poll.

## Releases

- [x] Releases and assets on both backends, streamed upload/download, external assets (Forgejo), backend-specific options. Increment 6.
- [ ] Asset `label` (GitHub) and editing an external asset's URL (Forgejo) are not exposed.
- [ ] `List` filters (Forgejo `draft`, `pre-release`, `q`) are not exposed.
- [ ] Tags are kept when a release is deleted; delete them with `Tags().Delete`.
- [ ] Undecided, much later: a `Tags().Release(tag, opts)` shortcut. `Releases().Create` with `TagName` already creates the tag if needed or releases an existing one.

## Pull requests

- [x] Pull requests (list, get, create, edit, close/reopen, merge, is-merged), review requests and reviews on both backends. Increment 7.
- [x] GitHub draft status changes and auto-merge via a minimal GraphQL client; `CancelAutoMerge` on both backends. Enhancement 1.
- [ ] GitHub auto-merge success path is unit-tested only: it needs a required status check, i.e. branch protection (public repositories on free plans).
- [ ] Forgejo drafts rely on the default `WORK_IN_PROGRESS_PREFIXES` (`WIP:`, `[WIP]`); a mismatch is reported, not worked around.
- [ ] Not exposed yet: line-level review comments, diff/patch, changed files, commits, update-branch, labels/milestones/assignees (wait for the issues area), `ListPROptions` head/base filters (GitHub only).
- [ ] `IsMerged` reports false for a pull request that does not exist (both servers answer 404).

## Webhooks

- [x] Repository and organisation webhooks on both backends; `webhook` sub-package for receiving (verification, forge detection, typed push/pull_request/release). Increment 8.
- [ ] Forgejo 15: `POST /admin/hooks` creates a *default* webhook (copied into every new repository), not a system hook, and `GET /admin/hooks` only lists system hooks. `SystemWebhooks` was dropped; possible `DefaultWebhooks` (get/create/edit/delete by ID only). Consider reporting upstream.
- [ ] The production instance has default webhooks (Mattermost, Jenkins) copied into every new repository, including test repositories: integration tests trigger them.
- [ ] Not exposed yet: Forgejo user-level hooks (`/user/hooks`), git hooks (`/hooks/git`), delivery history/redelivery, GitHub `insecure_ssl`, wildcard event subscriptions.
- [ ] Typed payloads beyond push/pull_request/release (issues, comments, reviews, create/delete). Forgejo review events arrive under Forgejo-specific names; check before typing them.

## Branches and tags

- [x] Branches (list with default flag, get, create from branch/tag/commit, rename, delete) and tags (list, get, create lightweight or annotated, delete) on both backends. Enhancement 2a.
- [x] Branch and tag protection: Forgejo protection rules, GitHub rulesets (shared subset + backend-specific options). GitHub tested on a short-lived public repository (free plans offer rulesets only there). Enhancement 2b.
- [ ] GitHub rulesets: individual users cannot be exempted (teams only, organisations only); exempted teams bypass every rule of the ruleset, not just pushes.
- [ ] GitHub: rulesets targeting several patterns, `~DEFAULT_BRANCH`, or set at organisation level are listed but cannot be addressed by pattern. Classic branch protection is not used.
- [ ] Forgejo tag protection requires at least one allowed user or team.
- [ ] Forgejo reports the commit message as `message` for lightweight tags; the library keeps it only for annotated tags (tag object ID differs from the commit).
- [ ] GitHub `Tags.List` does not report annotations (one extra request per tag); use `Get`.
- [ ] The integration tests' `CreateBranch` helpers could add their file commit through a future contents API.

## Issues

- [x] Issues (list without pull requests, get, create, edit, close/reopen, delete), labels on issues and pull requests, comments, repository labels and milestones on both backends. Enhancement 2c.
- [ ] Forgejo resolves label names to IDs with one label listing per call; organisation-level labels are not found by name.
- [ ] Forgejo does not paginate issue comments (`ListComments` returns them all).
- [ ] Not exposed yet: issue locking, reactions, timeline, attachments, due dates, pinned issues, issue types/fields (GitHub), state reasons (GitHub).
- [ ] Unit-test coverage of the GitHub backend dropped to ~76% (labels/milestones list/get are covered by the integration checks only).

## API surface

- [ ] Optional interfaces for Forgejo extras: mirrors, push mirrors, migrate (`CapMirrors`, `CapMigrate` already declared).
- [ ] Forgejo-only create options (`trust_model`, `object_format`, `readme`) on `CreateRepoOptions`.
- [x] Forgejo repo create/edit options only have `private`; `VisibilityInternal` returns `ErrNotSupported`. Responses carry a read-only `internal` flag, mapped to `VisibilityInternal`. Resolved in increment 3.
- [ ] Cache user-vs-org lookups (`GET /orgs/{name}`), currently one extra request per `Create`/`List` with an owner.
- [ ] Renaming a repo leaves a redirect on Forgejo; check whether `Get` on the old name should be documented.

## Admin (Forgejo)

- [x] 5a Users: list, get, create, edit, rename, delete (purge). Increment 5a.
- [x] 5b Orgs (get, list, list all, create incl. for another user, edit, rename, delete) and members (list, check, remove). Increment 5b.
- [ ] Forgejo returns HTTP 500 (not 4xx) for "org still owns repositories" and "last member of owner team". Consider mapping these to a typed error, or reporting upstream.
- [x] 5c Teams (list, get, get by name, create, edit, delete), members and repos. `TeamAccess` hides Forgejo's permission/units_map interplay; owner team protected by `ErrOwnerTeam`. Increment 5c.
- [ ] Forgejo lets an admin delete an org's owner team (204) and rename it; the library refuses both. Consider reporting upstream.
- [ ] Forgejo 15.0.x bug: `PATCH /admin/users/{u}` ignores `"visibility":"public"` (zero value of its enum). `Users.Edit` returns `ErrNotSupported` with the updated user when the server keeps the old value. Consider reporting upstream at codeberg.org/forgejo/forgejo.
- [ ] Interfaces grow as areas are added (e.g. `Admin` gained `Orgs()`/`Teams()`): breaks external implementations such as mocks. Acceptable before the first tagged release; freeze after.

## Open items from DESIGN-NOTES §8

- [x] Forgejo draft PRs: a title prefix (`WIP:`/`[WIP]`), no API field; drafts cannot be merged (405). Increment 7.
- [x] Forgejo `mergeable` is a plain bool, set immediately; GitHub's is nullable (`PullRequest.Mergeable *bool`). Increment 7.
- [x] Forgejo pagination: `page`/`limit`, `Link` + `X-Total-Count` headers; defaults are instance settings (`/api/v1/settings/api`: 30 default, 50 max on git.famillegratton.net). Resolved in increment 2.
- [x] Webhook signatures: both forges send `X-Hub-Signature-256: sha256=<hex HMAC-SHA256>`; Forgejo also `X-Forgejo-Signature` (bare hex) and GitHub-style event headers. Verified against real deliveries from both. Increment 8.
- [ ] Forgejo asset upload limits and content-type handling.
- [ ] Changes to Forgejo user/org/team endpoints in recent majors (quota APIs etc.).
- [x] `go-github`: pinned at v93.0.0 (declares go 1.26.0). New majors change the import path; review periodically. Resolved in increment 4.
- [x] Minimum Go version: 1.27.2. Minimum Forgejo version: 11.0.10.
- [ ] Exact set of "Other" areas beyond the five priorities.

## Testing

- [x] Integration tests: `-tags integration` + `GITOPSLIB_FORGEJO_{URL,TOKEN,ORG}` (token scopes: repository, organization, user, issue — read and write); repos prefixed `gitopslib-it-`, cleanup deletes only those. Added in increment 3.
- [x] Shared contract checks in `internal/contracttest`, run by both backends' integration tests. Added in increment 4.
- [x] GitHub user-namespace: covered by setting `GITOPSLIB_GITHUB_ORG` to the account's own login. Increment 4.
- [x] Disposable Forgejo for admin tests: `scripts/forgejo-testenv.sh start|stop` (Docker, loopback only, writes `.env.testenv`). Admin tests refuse non-loopback URLs. Increment 5a.
- [ ] CI — **on hold until really needed** (no Forgejo/GitHub Actions for now). When picked up: (1) checks: gofmt, go vet (with and without the integration tag), go test -race; (2) Forgejo integration against the disposable container only, never production (testenv script would also create a CI org and write `GITOPSLIB_FORGEJO_*`); (3) GitHub integration on manual trigger only (token as a CI secret; rate limits; public repo for protection checks). Platform undecided (Jenkins, Forgejo Actions, GitHub Actions).

## Very long term: more forges

Not planned; no accounts or test environments yet. Assessment from API knowledge, not verified against current docs (points marked "verify" especially).

### GitLab

- [ ] GitLab backend. Maps well: projects, branches, tags, issues, labels, milestones, comments, merge requests (drafts = `Draft:` title prefix, approvals), releases tied to tags, admin users/groups (self-managed), and a token API (personal, project and group tokens: create, rotate, revoke) that fits the planned `Tokens` interface better than GitHub.
- Needs adapting:
  - nested groups (`acme/platform/backend`); projects addressed by encoded path or numeric ID; listing by owner and the user/organisation lookup differ.
  - merge methods (merge commit, semi-linear, fast-forward) are a project setting, not a per-merge choice; squash is a per-merge flag. `MergeOptions.Method` would mostly be `ErrNotSupported` or need a project-level setting.
  - release assets are links; files are uploaded separately (package registry or project uploads), so `UploadAsset` would be two steps.
  - no teams: access via group/project membership with roles (Developer, Maintainer…). `Teams` would be `ErrNotSupported` or need a membership interface.
  - protection: protected branches/tags with role-based access levels; approval rules are paid-tier.
  - webhooks: traditionally a plain secret in `X-Gitlab-Token`, not an HMAC (newer versions may sign: verify); the `webhook` package needs a GitLab path.
- Official Go SDK: `gitlab.com/gitlab-org/api/client-go` (could be used like go-github).
- Testing: GitLab CE runs in Docker like the Forgejo test container (heavier: ~4 GB RAM, minutes to start); a free gitlab.com account covers the hosted side.

### Bitbucket

- [ ] Bitbucket Cloud and Bitbucket Data Center have different APIs (REST 2.0 vs REST 1.0): two backends.
- Gaps (Cloud): no releases (only a "Downloads" area); no labels or milestones on pull requests; minimal issue tracker (Jira is pushed instead); merge strategies merge commit, squash, fast-forward; draft PRs only recently (verify); three-level structure workspace / project / repository (`owner` = workspace); branch permissions and merge checks map partially to the protection model; webhooks: different event header (`X-Event-Key`) and payloads, HMAC signing added fairly recently (verify).
- Many shared interfaces would return `ErrNotSupported`: acceptable with capabilities, but limited value as a common backend.
- Testing: Cloud with a free account; Data Center needs a licence (evaluation licences and a Docker image exist, heavy and time-limited).

### Design impact

- A GitLab backend would likely push shared types to become less GitHub/Forgejo-shaped: `MergeMethod`, `TeamAccess`, `HookEvent`.
- Suggested order if ever done: GitLab first; Bitbucket Cloud maybe; Data Center only on real demand.

### Decision grid

| Backend | Fit | Effort | Testable locally |
|---|---|---|---|
| GitLab | good | ≈ one backend (like Forgejo or GitHub) | ✅ GitLab CE in Docker |
| Bitbucket Cloud | partial (no releases, labels, milestones) | medium, many gaps | free account |
| Bitbucket Data Center | partial, separate API | medium-large | evaluation licence |
