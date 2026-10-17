# TODO

Deferred items and open questions. Remove entries when done; note the increment that resolved them.

## Transport (Forgejo)

- [ ] Multipart upload for release assets (with releases increment).
- [ ] `Sudo` header option: admin acting as another user.
- [ ] Basic auth with TOTP (`X-FORGEJO-OTP`) for accounts with 2FA.
- [ ] Retries / backoff on transient errors (5xx, connection reset). Forgejo sends no rate-limit headers.
- [ ] `/api/forgejo/v1` namespace client, for Forgejo-only endpoints. Its swagger was not found at `/api/forgejo/v1/swagger.json` on 15.0.2; locate it.

## API surface

- [ ] Optional interfaces for Forgejo extras: mirrors, push mirrors, migrate (`CapMirrors`, `CapMigrate` already declared).
- [ ] Forgejo-only create options (`trust_model`, `object_format`, `readme`) on `CreateRepoOptions`.
- [x] Forgejo repo create/edit options only have `private`; `VisibilityInternal` returns `ErrNotSupported`. Responses carry a read-only `internal` flag, mapped to `VisibilityInternal`. Resolved in increment 3.
- [ ] Cache user-vs-org lookups (`GET /orgs/{name}`), currently one extra request per `Create`/`List` with an owner.
- [ ] Renaming a repo leaves a redirect on Forgejo; check whether `Get` on the old name should be documented.

## Open items from DESIGN-NOTES §8

- [ ] Forgejo draft PR support (swagger).
- [x] Forgejo pagination: `page`/`limit`, `Link` + `X-Total-Count` headers; defaults are instance settings (`/api/v1/settings/api`: 30 default, 50 max on git.famillegratton.net). Resolved in increment 2.
- [ ] Webhook signature header names, algorithm and prefix, Forgejo vs GitHub, tested against real deliveries.
- [ ] Forgejo `mergeable` semantics and merge request body fields.
- [ ] Forgejo asset upload limits and content-type handling.
- [ ] Changes to Forgejo user/org/team endpoints in recent majors (quota APIs etc.).
- [ ] `go-github` current major version and pinning policy.
- [x] Minimum Go version: 1.27.2. Minimum Forgejo version: 11.0.10.
- [ ] Exact set of "Other" areas beyond the five priorities.

## Testing

- [x] Integration tests: `-tags integration` + `GITOPSLIB_FORGEJO_{URL,TOKEN,ORG}`; repos prefixed `gitopslib-it-`, cleanup deletes only those. Added in increment 3.
- [ ] CI job running integration tests against a disposable Forgejo container.
