# Reports for Forgejo upstream

Ready to file at https://codeberg.org/forgejo/forgejo/issues (items 37 and 97 in `TODO.md`). Each was reproduced on disposable instances of **15.0.11, 16.0.7 and 17.0.1** on 2026-11-05, unless it says otherwise. A search of the tracker found no duplicate, but its search is imprecise, so look again before filing. Remove a report from this file once it is filed.

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
