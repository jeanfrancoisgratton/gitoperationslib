# Post-v15 tasks

What Forgejo's API gained after 15.0.2, compared from the swagger files in `__docs` (`forgejo-swagger-15.0.2.json`, `forgejo-swagger-17.0.1.json`) and the published swagger of 16.0.7. Nothing was removed. Item numbers continue those of `TODO.md`. Remove entries when done, or note the release that resolved them.

The swagger only describes the API: how these calls really behave is still to be checked, on a disposable container ([`__scripts/forgejo-testenv.sh`](../__scripts/forgejo-testenv.sh)).

## Ground rules

- **The minimum stays Forgejo 15** (`forgejo.MinimumVersion`). A feature that needs a later version returns `ErrNotSupported` on an older server, with the version it needs in the reason, based on `Client.Version()`.
- **The production server runs 17.0.1**, since 2026-11-05 (upgraded in place from 15.0.2). So 17 is what really gets used, and 15 is what must keep working.
- **Test both:** run live checks on 15 and on 17 (`GITOPSLIB_TESTENV_IMAGE=codeberg.org/forgejo/forgejo:17`). The local mirror tests skip on 17, which refuses plain `http://` remotes.
- **Capabilities:** a capability bit says a backend can do something, not that this server's version can. Decide how the two fit together with item 100, the first to need it.
- New areas land as optional interfaces, never as methods on existing ones (item 1 in `TODO.md`).

## Action items

- [ ] **100 →** **Workflow runs**, the biggest gap: `CI` only manages secrets and variables today, so this is a new optional interface. See what GitHub Actions and Bitbucket Pipelines offer for the shared shape.
  - v16: list a run's jobs (`GET .../actions/runs/{run_id}/jobs`), a job's logs (`GET .../actions/jobs/{job_id}/logs`), a run's logs as a ZIP (`GET .../actions/runs/{run_id}/logs`), cancel (`POST .../runs/{run_id}/cancel`), delete a completed run (`DELETE .../runs/{run_id}`).
  - v16: artifacts: of a repository (`GET .../actions/artifacts`), of a run (`GET .../runs/{run_id}/artifacts`), get, delete, download (`GET .../artifacts/{artifact_id}/zip`). Type `ActionArtifact`.
  - v17: a single job with its steps (`GET .../actions/jobs/{job_id}`, type `ActionRunJobStep`), rerun a job and its dependents (`POST .../jobs/{job_id}/rerun`), rerun a run (`POST .../runs/{run_id}/rerun`).
  - `ActionRunJob` gained `run_id` (v16), `html_url` and `steps` (v17). Workflow dispatch can now answer 400 (v16).
- [x] **101 →** (1.5.0: `AdminTokens`; an ordinary admin token works) **Admin-managed tokens** (v16): `GET` and `POST /admin/users/{username}/tokens`, `DELETE .../tokens/{token}`. May lift the limit in item 10 of `TODO.md`, where creating or deleting a token needs a password, even for an admin. Test first whether an admin's token is accepted here.
- [x] **102 →** (1.5.0) **List a user's own secrets** (v17): `GET /user/actions/secrets`. `Secrets().List` on `UserCI` returns `ErrNotSupported` today; it should work on 17 and keep refusing below that. Update the "cannot be listed" notes in `USAGE.md` and `support_grid.md`.
- [x] **103 →** (1.5.0: on all three forges; 15 ignores them, so it is refused there) **Filter pull requests by branch** (v16): `base` and `head` on `GET .../pulls`. `ListPROptions` only has `State` today. See what GitHub and Bitbucket offer before choosing the options' shape.
- [x] **104 →** (1.5.0: `IssueLocks`; `reason` is required, contrary to the swagger) **Lock and unlock an issue** (v17): `PUT` and `DELETE .../issues/{index}/lock`, type `IssueLockOption`. `Issue.Locked` can only be read today. GitHub also has this, Bitbucket has no issues.

## Webhooks (done in 1.4.0)

The swagger files do not show these, as they do not list event names. Checked on real servers.

- [x] **112 →** **Workflow events** (v17): `workflow_run_*` and `workflow_job_*`, seven statuses each, and `action_run_*` deprecated, without `action_run_recover`. 15 and 16 drop them all. `HookEventWorkflowRun`, `HookEventWorkflowJob`, `HookEventAll` by version, `Event.WorkflowRun` and `Event.WorkflowJob`, tested with a runner against real deliveries. `workflow_run_skipped` was not seen delivered. Prod's `action_run_*` subscriptions are unchanged: migrating them is a separate decision, as receivers must handle the new format first.
- [x] **113 →** `Hook.config` is marked deprecated (v17). The library reads the top-level `url` and `content_type`, present on 15, 16 and 17. Creating and editing still have to use `config`, as `CreateHookOption` has nothing else.

## Small additions

- [x] **105 →** (1.5.0) New fields to report (v16): `AccessToken.created_at`, `Organization.created`, `PublicKey.updated_at` and `verified` (`SSHKey`). Zero on older servers, which needs no version check.
- [x] **106 →** (1.5.0: `ExtraLines`, on all three forges; confirmed live) Multi-line review comments (v16): `extra_lines_count` on `CreatePullReviewComment` and `PullReviewComment`. It appears to allow them, to be confirmed. Would touch `CreateReviewCommentOptions` and `ReviewComment`.
- [x] **107 →** (1.5.0: `Branches().Compare` sends both, as it only needs the count; `files=false` also works on 15 and 16) Compare options (v17): `files` and `verification` on `GET .../compare/{basehead}`. Check whether they can save work where the library compares branches.
- [x] **108 →** (1.5.0) `is_2fa_enabled` filter on `GET /admin/users` (v16): `UserFilters`, as `Users.List` cannot take a filter without changing its signature. 15 ignores the filter and returns everyone, so it is refused there.
- [x] **109 →** (1.5.0) The 409 from `PUT /teams/{id}/members/{username}` (v16) means an invitation is already pending. With `[service] ADD_MEMBERS_BY_INVITATIONS`, the first attempt on someone outside the organisation answers 204, but only invites them. `AddMember` checks, and returns `ErrInvitationPending`. Checked by hand on 17.0.1, as the setting cannot be changed per test.

## Set aside

Not planned unless a need comes up:

- `POST /user/activitypub/follow` (v16).
- `GET /actions/run` (v16), which only applies to a token from inside a running workflow.
- `force_overwrite_new_branch` on file changes (v16), formerly item 110: dropped on 2026-11-05, as nothing needs it and the library has no file-writing API.
- New migration sources: `forgejo` and `pagure` (v16), `bitbucketdc` (v17). These only matter if item 58 in `TODO.md` (migration) goes ahead.
