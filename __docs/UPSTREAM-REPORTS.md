# Forgejo API findings

**Not being filed:** the procedure to file at Codeberg is not worth it, so this is only a record of how Forgejo's API behaves, and of why the library works as it does (items 37, 97 and 111 in `TODO.md`). Each was reproduced on disposable instances of **15.0.11, 16.0.7 and 17.0.1** on 2026-11-05, unless it says otherwise.

Throughout, `$F` is the instance's `/api/v1`, and `$AUTH` is `Authorization: token <a site admin's token>`.

---

## 1. API: system webhooks need an undocumented string flag, and nothing tells the two kinds of admin hook apart

`/admin/hooks` handles both system and default webhooks, but the API documentation does not explain how to choose.

**Creating.** `POST /admin/hooks` creates a default webhook unless `config` holds the string `is_system_webhook` set to `"true"`. Neither that key nor the distinction is in the swagger description of `CreateHookOption`. A top-level `is_system_webhook` field, the obvious guess, is silently ignored:

| Body of `POST /admin/hooks` | `GET /admin/hooks` lists it | Copied into a new repository |
|---|---|---|
| no flag | no | yes |
| `"is_system_webhook": true` | no | yes |
| `"config": {"is_system_webhook": "true", …}` | yes | no |
| `"config": {"is_system_webhook": "false", …}` | no | yes |

```sh
curl -X POST -H "$AUTH" -H 'Content-Type: application/json' "$F/admin/hooks" -d '{
  "type": "forgejo", "events": ["push"], "active": true,
  "config": {"url": "https://example.com/hook", "content_type": "json", "is_system_webhook": "true"}}'
```

A hook created this way does deliver for repositories of other users, as a system webhook should. The table was checked on 15.0.11 and 17.0.1, and the delivery on 17.0.1.

**Reading.** `GET /admin/hooks` lists only system webhooks, and default webhooks cannot be listed at all. `GET /admin/hooks/{id}` returns both, with identical fields, so a client holding an ID can only tell the kind by searching the list.

**Expected:** document the flag or make it a real field of `CreateHookOption`, reject or honour the top-level field, report the kind in `Hook`, and offer a way to list default webhooks.

### `PATCH /admin/hooks/{id}` answers 500 for a repository webhook's ID

```sh
curl -H "$AUTH" "$F/admin/hooks/$REPO_HOOK_ID"                      # 404
curl -X PATCH -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"active": false}' "$F/admin/hooks/$REPO_HOOK_ID"             # 500 "webhook does not exist [id: 1]"
```

**Expected:** 404, like the `GET`.

---

## 2. API: `limit` is ignored when `page` is absent, and `/push_mirrors` links a next page that repeats items

On some list endpoints, `limit` only applies when `page` is present. With three of each item:

| Request | Items | `Link: rel="next"` |
|---|---|---|
| `/repos/{o}/{r}/labels?limit=1` | 3 | no |
| `/repos/{o}/{r}/labels?limit=1&page=1` | 1 | no |
| `/repos/{o}/{r}/milestones?limit=1` | 3 | no |
| `/repos/{o}/{r}/milestones?limit=1&page=1` | 1 | no |
| `/repos/{o}/{r}/hooks?limit=1` | all | no |
| `/repos/{o}/{r}/hooks?limit=1&page=1` | 1 | no |
| `/repos/{o}/{r}/push_mirrors?limit=1` | 3 | **yes** |
| `/repos/{o}/{r}/push_mirrors?limit=1&page=1` | 1 | no |

`/repos/{o}/{r}/keys` also ignored the limit on 15.0.11; it was not rechecked on 16 and 17. Other lists, such as issues, branches, tags, releases and collaborators, do apply it.

On `/push_mirrors`, the combination loses data integrity for a client following links: the first response holds every mirror and points to `page=2`, which returns mirrors already seen.

`page=1` responses also lack the `Link` header on these endpoints, even with more pages to read; only `X-Total-Count` tells a client there is more.

**Expected:** `limit` applies with or without `page`, and `Link` is sent consistently.

---

## 3. API: `/push_mirrors` errors without a usable message

The message is a Go formatting of a nil error:

```sh
curl -H "$AUTH" "$F/repos/$O/$R/push_mirrors/remote_mirror_nope"              # 404 {"message":"%!s(<nil>)"}
curl -X DELETE -H "$AUTH" "$F/repos/$O/$R/push_mirrors/remote_mirror_nope"    # 404 {"message":"%!s(<nil>)"}
curl -X POST -H "$AUTH" -H 'Content-Type: application/json' "$F/repos/$O/$R/push_mirrors" \
  -d '{"remote_address": "https://example.com/x/y.git", "interval": "1m"}'    # 400 {"message":"%!s(<nil>)"}
```

The last one is an interval below `[mirror] MIN_INTERVAL`.

Related, and checked on 17.0.1: a plain `http://` remote gets 400 `{"message":"Unknown error"}`, although `POST /repos/migrate` with the same address explains that unencrypted transfer protocols are disabled, in a 500 "Unknown error type (ErrInvalidCloneAddr)". On 15.0.11, a host that does not resolve gets 401 "Permission denied", which reads as a problem with the caller's token.

**Expected:** a message saying what is wrong, and a 4xx with a clear message for a refused address.

---

## 4. API: `PATCH` on a webhook resets what it does not name, and ignores a new secret

Checked on 15.0.11 and 17.0.1, for a repository's webhook. `routers/api/v1/utils/hook.go`, `editHook`, explains all of it.

```sh
curl -X POST -H "$AUTH" -H 'Content-Type: application/json' "$F/repos/$O/$R/hooks" -d '{
  "type": "forgejo", "active": true, "events": ["push", "release"], "branch_filter": "main",
  "authorization_header": "Bearer x",
  "config": {"url": "https://example.com/hook", "content_type": "json", "secret": "s1"}}'

curl -X PATCH -H "$AUTH" -H 'Content-Type: application/json' -d '{"active": false}' "$F/repos/$O/$R/hooks/$ID"
```

| Sent in the `PATCH` | Result |
|---|---|
| no `events` | `events` becomes `["push"]`, so subscriptions are lost |
| no `branch_filter` | the filter is erased |
| `"config": {"secret": "s2"}` | answers 200, and deliveries stay signed with `s1` |
| `"authorization_header": ""` | answers 200, and the header stays |
| `events` adding or removing `package`, `action_run_*` or `workflow_*` | answers 200, and those stay as they were |

The secret is the serious one: someone rotating a leaked secret sees success while the old one stays valid.

**Expected:** a field left out stays as it is, and `secret` and an empty `authorization_header` take effect, or are refused. Until then, document it in the swagger.

---

## 5. API: webhook event names do not round-trip

Checked on 15.0.11 and 17.0.1. Reading a webhook's `events` and sending them back should change nothing, but:

| `events` when creating | Reported back | The same list sent back gives |
|---|---|---|
| `["pull_request_review_approved"]` | `[]` | `["push"]`, via the reset in report 4 |
| `["pull_request_review"]` | `["pull_request_review_approved", "pull_request_review_rejected"]` | `[]` |
| `["issues_only"]` | `["issues"]` | all five issue events |
| `["pull_request_only"]` | `["pull_request"]` | all ten pull request events |

The names the API reports are not all names it accepts, and `issues` means something else in each direction. Unknown names are also dropped without an error, which is how a webhook ends up subscribed to nothing.

**Expected:** accept the names it reports, with the same meaning, and answer 422 for an unknown event.

---

## 6. API: Telegram, Matrix, Packagist and SourceHut webhooks lose their settings

Checked on 17.0.1 for Telegram, Matrix and Packagist. `addHook` only reads `config` for Slack, while the others keep their settings in `metadata`, which `CreateHookOption` has no field for. So a webhook of these types created through the API cannot deliver, and no error says so:

```sh
curl -X POST -H "$AUTH" -H 'Content-Type: application/json' "$F/user/hooks" -d '{
  "type": "telegram", "active": true, "events": ["push"],
  "config": {"url": "https://example.com/t", "content_type": "json", "bot_token": "T", "chat_id": "C"}}'
# 201, with "metadata": {"bot_token": "", "chat_id": "", "thread_id": ""}
```

The swagger's list of types for `CreateHookOption` also leaves out `matrix` and `sourcehut_builds`, which the server accepts. Separately, `GET` returns `metadata.bot_token`, `metadata.api_token` and `authorization_header` in clear, unlike the webhook's secret.

**Expected:** accept these settings, or refuse the type with 422.
