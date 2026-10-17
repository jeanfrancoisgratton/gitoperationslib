# TODO

Deferred items and open questions. Remove entries when done; note the increment that resolved them.

## Transport (Forgejo)

- [ ] Multipart upload for release assets (with releases increment).
- [ ] `Sudo` header option: admin acting as another user.
- [ ] Basic auth with TOTP (`X-FORGEJO-OTP`) for accounts with 2FA.
- [ ] Retries / backoff on transient errors (5xx, connection reset). Forgejo sends no rate-limit headers.
- [ ] `/api/forgejo/v1` namespace client, for Forgejo-only endpoints. Its swagger was not found at `/api/forgejo/v1/swagger.json` on 15.0.2; locate it.

## Transport (GitHub)

- [ ] Rate-limit handling: go-github returns `RateLimitError` (converted to `APIError` with reset time); consider waiting/retrying or exposing the reset time as a field.
- [ ] GitHub returns 409 "conflicting repository operation is still in progress" for edits/deletes right after a create with `AutoInit`. Contract tests retry; decide whether the library should retry itself.
- [ ] Org-repo endpoints with an actual GitHub org are untested (the test account uses the personal namespace as "org"); unit tests cover the `/orgs/{org}/repos` path.
- [ ] `DefaultBranch` on create returns `ErrNotSupported`; possible emulation (create with `AutoInit`, then rename branch) if needed.

## API surface

- [ ] Optional interfaces for Forgejo extras: mirrors, push mirrors, migrate (`CapMirrors`, `CapMigrate` already declared).
- [ ] Forgejo-only create options (`trust_model`, `object_format`, `readme`) on `CreateRepoOptions`.
- [x] Forgejo repo create/edit options only have `private`; `VisibilityInternal` returns `ErrNotSupported`. Responses carry a read-only `internal` flag, mapped to `VisibilityInternal`. Resolved in increment 3.
- [ ] Cache user-vs-org lookups (`GET /orgs/{name}`), currently one extra request per `Create`/`List` with an owner.
- [ ] Renaming a repo leaves a redirect on Forgejo; check whether `Get` on the old name should be documented.

## Admin (Forgejo)

- [x] 5a Users: list, get, create, edit, rename, delete (purge). Increment 5a.
- [ ] 5b Orgs and members.
- [ ] 5c Teams, team members and team repos.
- [ ] Forgejo 15.0.x bug: `PATCH /admin/users/{u}` ignores `"visibility":"public"` (zero value of its enum). `Users.Edit` returns `ErrNotSupported` with the updated user when the server keeps the old value. Consider reporting upstream at codeberg.org/forgejo/forgejo.
- [ ] `Admin` gains `Orgs()` and `Teams()` in 5b/5c: adding interface methods breaks external implementations (mocks); acceptable before the first tagged release.

## Open items from DESIGN-NOTES §8

- [ ] Forgejo draft PR support (swagger).
- [x] Forgejo pagination: `page`/`limit`, `Link` + `X-Total-Count` headers; defaults are instance settings (`/api/v1/settings/api`: 30 default, 50 max on git.famillegratton.net). Resolved in increment 2.
- [ ] Webhook signature header names, algorithm and prefix, Forgejo vs GitHub, tested against real deliveries.
- [ ] Forgejo `mergeable` semantics and merge request body fields.
- [ ] Forgejo asset upload limits and content-type handling.
- [ ] Changes to Forgejo user/org/team endpoints in recent majors (quota APIs etc.).
- [x] `go-github`: pinned at v93.0.0 (declares go 1.26.0). New majors change the import path; review periodically. Resolved in increment 4.
- [x] Minimum Go version: 1.27.2. Minimum Forgejo version: 11.0.10.
- [ ] Exact set of "Other" areas beyond the five priorities.

## Testing

- [x] Integration tests: `-tags integration` + `GITOPSLIB_FORGEJO_{URL,TOKEN,ORG}`; repos prefixed `gitopslib-it-`, cleanup deletes only those. Added in increment 3.
- [x] Shared contract checks in `internal/contracttest`, run by both backends' integration tests. Added in increment 4.
- [x] GitHub user-namespace: covered by setting `GITOPSLIB_GITHUB_ORG` to the account's own login. Increment 4.
- [x] Disposable Forgejo for admin tests: `scripts/forgejo-testenv.sh start|stop` (Docker, loopback only, writes `.env.testenv`). Admin tests refuse non-loopback URLs. Increment 5a.
- [ ] CI job running integration tests against the disposable container (script is reusable); also run the repo contract checks there.
