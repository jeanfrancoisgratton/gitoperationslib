# Support grid

Support level of every feature, per backend, as of 2026-10-30. Backed by the integration checks run against Forgejo 15.0.2 (and 15.0.10 for the admin checks), github.com and Bitbucket Cloud unless marked †.

| Mark | Meaning |
|---|---|
| ✅ | Supported |
| ⚠️ | Supported with a caveat (see the note) |
| ❌ | Not available on this forge: the call returns `ErrNotSupported` |
| — | Does not apply |
| † | Covered by unit tests only (not exercised against the live service) |

Backends: **Forgejo** (≥ 11.0.10, primary), **GitHub** (github.com, GitHub Enterprise Server via `WithEnterpriseURL`), **Bitbucket** (Bitbucket Cloud, REST 2.0).

## Summary

| Area | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Repositories | ✅ | ✅ | ✅ owner = workspace |
| Branches and tags | ✅ | ✅ | ✅ no branch rename |
| Branch protection | ✅ protection rules | ✅ rulesets (free plans: public repositories) | ✅ branch restrictions |
| Tag protection | ✅ | ✅ rulesets | ❌ |
| Releases and assets | ✅ plus external assets | ✅ | ❌ |
| Pull requests and reviews | ✅ | ✅ drafts and auto-merge via GraphQL | ✅ no reopening, no auto-merge |
| Issues, labels, milestones, comments | ✅ | ✅ | ❌ issue tracker retired by Atlassian |
| Webhooks (repository, organisation/workspace) | ✅ | ✅ | ✅ |
| Receiving and verifying deliveries | ✅ | ✅ | ⚠️ from documentation, not yet checked against live deliveries |
| Reading users, organisations, teams (`Directory`) | ✅ | ✅ | ⚠️ the authenticated user (`Me`) only |
| Managing users, organisations, teams (`Admin`) | ✅ | — | — |
| Own account: e-mail addresses, SSH and GPG keys (`Account`) | ✅ | ✅† (needs account permissions on the token) | ⚠️ e-mail addresses read-only |

## Connecting

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Token authentication | ✅ `Authorization: token` | ✅ bearer | ✅ bearer (access tokens); Atlassian API tokens through basic auth with the account e-mail (`APIToken`) |
| Username + password / app password | ✅ | ❌ (GitHub removed it) | ✅ |
| Two-factor code with basic auth (`BasicAuthOTP`, `BasicAuthOTPFunc`) | ✅ | — | — |
| Acting as another user (`WithSudo`, site admins) | ✅ | ❌ | ❌ |
| Anonymous access | ✅ | ✅ | ✅ |
| Self-hosted instances | ✅ any base URL | ✅ GitHub Enterprise Server | ❌ Cloud only (Data Center is a different API) |
| Server version check | ✅ refuses < 11.0.10 | — | — |
| Custom HTTP client, user agent | ✅ | ✅ | ✅ |
| Retries on transient errors (`WithRetry`) | ✅ | ✅† also the 409 after `AutoInit` | ✅† |
| Rate-limit reset as data (`APIError.RetryAt`) | ✅ `Retry-After` (Forgejo sends no rate-limit headers) | ✅† | ✅† `Retry-After` |

## Repositories

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Get, delete | ✅ | ✅ | ✅ |
| List an owner's repositories | ✅ | ✅ | ✅ |
| List every accessible repository (`owner` empty) | ✅ | ✅ | ⚠️ lists each workspace; one page when there are several |
| Create in the user's namespace | ✅ | ✅ | ⚠️ `Owner` empty = the user's only workspace |
| Create in an organisation / workspace | ✅ | ✅ | ✅ (workspace's default project) |
| Create for another user | ✅ site admins | ❌ | — |
| Public / private | ✅ | ✅ | ✅ |
| `internal` visibility | ❌ | ✅ | ❌ |
| `AutoInit` (initial commit) | ✅ | ✅ | ⚠️ commits a `README.md` |
| Default branch on create | ✅ | ❌ (set it with `Edit`) | ⚠️ with `AutoInit` only |
| Gitignore templates | ✅ several | ⚠️ one | ❌ |
| License template | ✅ | ✅ | ❌ |
| Template repository flag | ✅ | ✅ | ❌ |
| Rename | ✅ | ✅ | ⚠️ the slug (and URLs) change |
| Edit description, website, visibility, default branch | ✅ | ✅ | ✅ |
| Archive | ✅ | ✅ | ❌ |
| `Repo.Empty` | ✅ | ❌ always false | ❌ always false |
| `Repo.ID` | ✅ | ✅ | ❌ always 0 (Bitbucket uses UUIDs) |

## Branches and tags

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List branches (default branch flagged) | ✅ | ✅ | ✅ |
| Get, delete a branch | ✅ | ✅ | ✅ |
| Create a branch from a branch, tag or commit | ✅ | ✅ | ✅ |
| Rename a branch | ✅ | ✅ | ❌ |
| `Branch.Protected` reported | ✅ | ✅ | ❌ always false |
| `Branch.LastCommit` (message, author, date) | ✅ | ⚠️ `Get` only | ✅ |
| Compare two refs (ahead / behind) | ✅ two requests | ✅ | ⚠️ counts the commits, 100 per request |
| Lightweight tags | ✅ | ✅ | ⚠️ Bitbucket always annotates; its generated message reads back as empty |
| Annotated tags | ✅ | ✅ | ✅ |
| List, get, delete tags | ✅ | ✅ | ✅ |
| Tag annotation in `List` | ✅ | ⚠️ `Get` only | ✅ |
| Tag archive URLs | ✅ | ✅ | ✅ |
| Change the default branch | ✅ `Repos().Edit` | ✅ `Repos().Edit` | ✅ `Repos().Edit` |

## Branch protection

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Implemented as | protection rules | repository rulesets | branch restrictions (one per setting) |
| Availability | all plans | public repositories on free plans | free plan (some settings Premium) |
| List, get, create, update, delete by pattern | ✅ | ✅ | ⚠️ `Update` is not atomic (delete, then recreate) |
| Glob patterns (`release/*`) | ✅ | ✅ | ✅ |
| Required approvals | ✅ | ✅ | ✅ |
| Dismiss stale approvals | ✅ | ✅ | ⚠️ Premium plans |
| Named required status checks | ✅ | ✅ | ❌ (Bitbucket only counts passing builds) |
| Require up-to-date branch | ✅ | ⚠️ with status checks only | ❌ |
| Require signed commits | ✅ | ✅ | ❌ |
| Require linear history | ❌ | ✅ | ❌ |
| Require conversation resolution | ❌ | ✅ | ❌ |
| Block deletion and force pushes (default) | ✅ | ✅ | ✅ |
| Allow deletion / force pushes | ❌ | ✅ | ✅ |
| Push allowlist: users | ✅ | ❌ | ✅ |
| Push allowlist: teams | ✅ | ⚠️ organisation teams; they bypass the whole ruleset | ⚠️† groups |
| Merge allowlist | ✅ | ❌ | ✅ |
| Approvals allowlist | ✅ | ❌ | ❌ |
| Block on rejected reviews / official review requests | ✅ | ❌ | ❌ |
| Protected / unprotected file patterns | ✅ | ❌ | ❌ |
| `ApplyToAdmins` | ✅ | ✅ (admin-role bypass when false) | ⚠️ no equivalent: ignored, reported true |
| Deleting a protected branch is refused (`ErrBranchProtected`) | ✅ HTTP 403 | ✅ HTTP 422 | ✅ HTTP 500, confirmed against the branch restrictions |

## Tag protection

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List, get, create, update, delete by pattern | ✅ | ✅ (tag rulesets) | ❌ |
| Allowlist: users | ✅ | ❌ | — |
| Allowlist: teams | ✅ | ✅ organisation teams | — |
| Empty allowlist | ❌ (at least one entry required) | ✅ blocks everyone, admins included | — |
| Protected tag creation is refused | ⚠️ allowlisted users may | ✅ | — |

## Releases and assets

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List, get, get by tag, latest | ✅ | ✅ | ❌ |
| Create (tag created on publish), edit, delete (tag kept) | ✅ | ✅ | ❌ |
| Drafts, prereleases | ✅ | ✅ | ❌ |
| Generated release notes | ❌ | ✅ | ❌ |
| "Make latest" flag | ❌ | ✅ | ❌ |
| Hide source archive links | ✅ | ❌ | ❌ |
| Upload assets (streamed, any `io.Reader`) | ✅ | ✅ | ❌ |
| List, get, rename, delete assets | ✅ | ✅ | ❌ |
| Download assets (streamed, private repositories too) | ✅ | ✅ | ❌ |
| External (linked) assets | ✅ | ❌ | ❌ |

## Pull requests and reviews

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List (open / closed / all), get | ✅ | ✅ | ✅ |
| Create | ✅ | ✅ | ✅ |
| Create from a fork (`user:branch`) | ✅ | ✅ | ❌ |
| Edit title, body, base | ✅ | ✅ | ✅ |
| Allow maintainer edits | ✅ | ✅ | ❌ |
| Create as draft | ⚠️ `WIP: ` title prefix (raw title returned) | ✅ | ✅ |
| Change draft status | ⚠️ title prefix | ✅ GraphQL | ✅ |
| Drafts cannot be merged | ✅ | ✅ | ✅ |
| Close | ✅ | ✅ | ✅ (decline) |
| Reopen | ✅ | ✅ | ❌ declines are final |
| Merge: merge commit | ✅ | ✅ | ✅ |
| Merge: squash | ✅ | ✅ | ✅ |
| Merge: rebase | ✅ | ✅ | ✅ |
| Merge: rebase then merge commit | ✅ | ❌ | ✅ |
| Merge: fast-forward only | ✅ | ❌ | ✅ |
| Mark as manually merged | ✅ | ❌ | ❌ |
| Merge only if the head is still at `HeadSHA` | ✅ | ✅ | ⚠️ checked just before merging |
| Delete the head branch after merging | ✅ | ⚠️ separate call; not for forks | ✅ |
| Auto-merge when checks pass | ✅ | ⚠️ GraphQL; repository must allow it and a required check must be pending | ❌ |
| Cancel auto-merge | ✅ | ✅† | ❌ |
| `IsMerged` | ✅ | ✅ | ✅ |
| `Mergeable` | ✅ | ⚠️ `nil` while GitHub computes it | ❌ always `nil` |
| Request / remove reviewers: users | ✅ | ✅ | ✅ |
| Request / remove reviewers: teams | ✅ | ✅ | ❌ |
| List reviews | ✅ | ✅ | ⚠️ verdicts only (approved / changes requested), no IDs |
| Approve, request changes, comment | ✅ | ✅ | ✅ |
| Review a specific commit | ✅ | ✅ | ❌ |
| Approving one's own pull request | refused by the server | refused by the server | allowed by the server |

## Issues, labels, milestones and comments

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List (pull requests excluded), get, create, edit | ✅ | ✅ | ❌ |
| Close, reopen | ✅ | ✅ | ❌ |
| Delete an issue | ✅ | ✅ GraphQL | ❌ |
| Labels and assignees on issues | ✅ | ✅ | ❌ |
| Labels on pull requests (shared numbering) | ✅ | ✅ | ❌ |
| Comments: list, create, edit, delete | ⚠️ `ListComments` returns all at once | ✅ | ❌ |
| Repository labels: list, get, create, edit, delete | ✅ | ✅ | ❌ |
| Milestones: list, get, create, edit, close, delete | ✅ | ✅ | ❌ |

## Webhooks

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Repository webhooks: list, get, create, edit, delete | ✅ | ✅ | ✅ |
| Organisation / workspace webhooks | ✅† | ✅† | ✅† |
| Webhook IDs | ✅ | ✅ | ⚠️ derived from Bitbucket's UUIDs |
| Ping (test delivery) | ⚠️ repositories only; a `push` event | ✅ | ❌ |
| Secret (signed deliveries) | ✅ | ✅ | ✅ |
| JSON payloads | ✅ | ✅ | ✅ |
| Form-encoded payloads | ✅ | ✅ | ❌ |
| Branch filter | ✅ | ❌ | ❌ |
| Authorization header | ✅ | ❌ | ❌ |
| Unknown event names | ⚠️ dropped by the server, reported as an error | ✅ rejected | ✅ rejected |
| Target URL | any | any | must resolve in DNS |
| Instance-wide (system) webhooks | ❌ (Forgejo 15 API creates default hooks instead) | — | — |

Events (`HookEvent`):

| Event | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| `push` | ✅ | ✅ | ✅ `repo:push` |
| `create`, `delete` (branch or tag) | ✅ | ✅ | ❌ (covered by `repo:push`) |
| `fork` | ✅ | ✅ | ✅ `repo:fork` |
| `release` | ✅ | ✅ | ❌ |
| `issues`, `issue_comment` | ✅ | ✅ | ❌ |
| `pull_request` | ✅ | ✅ | ✅ `pullrequest:created/updated/fulfilled/rejected` |
| `pull_request_review` | ✅ three review events | ✅ | ✅ approvals and change requests |
| `repository` | ✅ | ✅ | ✅ `repo:updated` |
| `wiki` | ✅ | ✅ `gollum` | ❌ |
| Backend-specific names | ✅ passed through | ✅ passed through | ✅ keys containing `:` passed through |

## Receiving webhooks (`webhook` package)

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Signature verification (HMAC-SHA256) | ✅ `X-Hub-Signature-256` / `X-Forgejo-Signature` | ✅ `X-Hub-Signature-256` | ⚠️ `X-Hub-Signature` (from documentation) |
| Forge and event detection | ✅ | ✅ | ⚠️ `X-Event-Key`, mapped to common names (`Event.Key` keeps the raw key) |
| `Push()` | ✅ | ✅ | ⚠️ first change of the push (from documentation) |
| `PullRequest()` | ✅ | ✅ | ⚠️ from documentation |
| `Release()` | ✅ | ✅ | ❌ |
| Form-encoded deliveries | ✅ | ✅ | — |
| Checked against live deliveries | ✅ | ✅ (real signatures) | ❌ synthetic fixtures |

## Reading users, organisations and teams (`Directory`)

Works with any token; the server filters what the caller may see. Forgejo checks run with a non-admin token.

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Authenticated user (`Me`): private fields, e-mail addresses, memberships, repositories, keys | ✅ plus settings | ✅ plus plan and private counts | ⚠️ workspaces as organisations; no teams, no GPG keys |
| User, organisation, team (by organisation and name) | ✅ | ✅ team = slug | ❌ |
| Authenticated user's organisations, another user's | ✅ | ⚠️ another user's: public memberships only | ❌ |
| Every visible organisation (`ListAllOrgs`) | ✅ | ⚠️ `since` cursor carried in `NextPage` | ❌ |
| Organisation members | ✅ (non-members: public members) | ✅ (non-members: public members) | ❌ |
| Organisation teams | ⚠️ members only | ⚠️ visible teams; 403 without organisation access | ❌ |
| Team members and repositories | ✅ | ✅ | ❌ |
| `UserInfo`, `OrgInfo`, `TeamInfo` (refused parts reported in `Incomplete`) | ✅ | ✅† organisation and team info | ❌ |
| Avatar, followers / following | ✅ | ✅ | — |
| Pronouns, interface language, starred count | ✅ | ❌ | — |
| Company, organisation web URL | ❌ | ✅ | — |

## Administration

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Users: list, get, create, edit, rename, delete (purge) | ✅ site admins | — | — (not implemented by choice) |
| Switch a user back to public visibility | ⚠️ Forgejo 15 ignores it; reported as `ErrNotSupported` | — | — |
| Organisations: list, get, create (also for another user), edit, rename, delete | ✅ | — | — |
| Organisation members: list, check, remove | ✅ | — | — |
| Teams: list, get, get by name, create, edit, delete | ✅ | — | — |
| Team members and repositories | ✅ | — | — |
| Per-unit team access (`TeamAccess`) | ✅ | — | — |
| Owner team protected (`ErrOwnerTeam`) | ✅ | — | — |

See [TODO.md](TODO.md) for open items and the reasons behind each limitation.

## Own account (`Account`)

| Feature | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| List e-mail addresses | ✅ | ✅† | ✅ |
| Add, delete e-mail addresses | ✅ | ✅† (verification e-mail sent) | ❌ refused with token authentication |
| List, get, add, delete SSH keys | ✅ | ✅† | ✅ `ID` derived from the UUID |
| Read-only SSH keys | ✅ | ❌ | ❌ |
| List, get, add, delete GPG keys | ✅ | ✅† | ✅ `ID` derived from the fingerprint; key must bear an account address |
| GPG verification by signed token | ✅ | ❌ | ❌ |

