# Changelog

All notable changes to **gitoperationslib** are documented in this file.

The format is based on Keep a Changelog (keepachangelog.com, version 1.1.0), and this project
adheres to Semantic Versioning (semver.org, version 2.0.0). Before 1.0.0, minor versions may
change the public API.

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
- `docs/USAGE.md` §3.7 (JSON).

### Changed
- `webhook.Event.Payload` is now `json.RawMessage` instead of `[]byte`, so it marshals as JSON
  rather than base64. It has the same underlying type, so `json.Unmarshal(ev.Payload, …)` and
  assignments to `[]byte` still compile.
- `Incomplete` is tagged `json:"-"`.

## [0.2.1] — 2026-10-23

### Added
- `Directory.Me`: the authenticated user, from the credentials, with everything the token may
  read: private profile fields, e-mail addresses, organisations, teams, repositories, SSH and GPG
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
- README rewritten (architecture, design choices, backend differences); `docs/USAGE.md` added,
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
- `docs/support_grid.md`: support level of every feature, per backend.
- `docs/CHANGELOG.md` (this file).
- `docs/forgejo-swagger-15.0.2.json`: Forgejo 15.0.2's API definition, for reference.
- `_dontexec` marker: the module is a library, not an executable.
- `docs/TODO.md`: next priorities (Forgejo access tokens, then GitHub App authentication),
  CI kept on hold, and an assessment of GitLab and Bitbucket as further backends.

### Changed
- `webhook.Verify` also accepts Bitbucket's `X-Hub-Signature` when it carries a `sha256=`
  signature (GitHub's legacy SHA-1 header is still ignored).
- Contract checks (`internal/contracttest`) now hold each backend to its documented gaps: an
  area a backend lacks must return `ErrNotSupported` instead of being skipped. Verified
  against Forgejo, github.com and Bitbucket Cloud.
- README: Bitbucket client setup, receiving Bitbucket webhooks, Bitbucket test variables;
  the feature and backend-difference grids moved to `docs/support_grid.md`.
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
- `scripts/forgejo-testenv.sh`: disposable local Forgejo in Docker for the admin and webhook
  delivery tests (admin tests refuse non-loopback URLs).
- README with worked examples for every area; `docs/TODO.md`.
