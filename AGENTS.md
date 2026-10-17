# OpenSentry agent notes

This is the project's log of mistakes and wrong assumptions. They already broke a build, a review, a test, or a user flow. Read this file at the start of every task in this repository, whether you are a person or a coding agent. Add an entry when you uncover another one. Correct an entry in place when the code changes. Do not delete history.

The product name is OpenSentry. The Go module is `github.com/zigamedved/OpenSentry`. The code and Compose files live in `cronsentry/` on purpose. The default branch is `master`. The default database name is `cronsentry`, not `opensentry`.

## How to add an entry

Copy this shape and put it under the matching heading. Lead with the date you found it.

```markdown
### YYYY-MM-DD — Short title

**Assumed:** What seemed true.
**Actually:** What happened, including the error or the user-visible result.
**Do this:** The rule to follow next time.
```

## Go, modules, and CI

### 2026-10 — `go get` rewrote the module to Go 1.26

**Assumed:** `go get golang.org/x/crypto/bcrypt` would add bcrypt and leave the rest of `go.mod` alone.
**Actually:** The latest `golang.org/x/crypto` needed Go 1.26, so `go.mod` became `go 1.26.0`. A later `go mod tidy` on that toolchain also added testify, `x/net`, and yaml even though this module does not import them. CI uses `actions/setup-go` with Go 1.22 and fails on a `go 1.26` line.
**Do this:** Keep `go 1.22.4` and `golang.org/x/crypto v0.32.0`. Do not run a bare `go get` of the latest `x/crypto`. Do not add testify. If `go.mod` moves, put the pin back before pushing.

### 2026-10 — `go build ./cmd` does not produce a binary

**Assumed:** `go build ./cmd` is the local and CI build, because the package directory is `cmd`.
**Actually:** Go names the binary `cmd`. That path is already the `cmd` directory, so the build stops with "output cmd already exists and is a directory". CI compiles with `go build -o /tmp/opensentry ./cmd`.
**Do this:** Use `go build -o /tmp/opensentry ./cmd` from `cronsentry/`.

### 2026-10 — `npm run lint` is not the frontend check

**Assumed:** A red `npm run lint` means the dashboard change is wrong and must be fixed before CI will pass.
**Actually:** `cronsentry/web/package.json` runs `eslint . --ext ts,tsx`. The project uses flat config (`eslint.config.js`), and current ESLint rejects `--ext` with `Invalid option '--ext'`. CI does not run lint. CI runs `npm ci && npm run build` (`tsc` and `vite build`) from `cronsentry/web`, plus `go test ./...` and the Go build above. API tests need Postgres. CI sets `DB_HOST=localhost`, `DB_PORT=5432`, `DB_USER=postgres`, `DB_PASSWORD=postgres`, and `DB_SSLMODE=disable`.
**Do this:** Judge the dashboard by `npm run build`. Leave the lint script alone unless the task is to repair lint. Do not treat a lint failure as a CI failure.

### 2026-10 — Schema init depends on the process working directory

**Assumed:** The API can be started from the repository root, and tests can be started from any directory, because the schema is inside the module.
**Actually:** `InitDatabase` reads `internal/db/schema.sql` relative to the current directory. Starting the API from `/workspace` fails before it listens. API behavior tests `chdir` to the directory that contains `go.mod` in `TestMain`. The dev server and `go test` both have to run from `cronsentry/`.
**Do this:** `cd cronsentry` before `go run ./cmd` or `go test ./...`. Schema changes belong in `schema.sql`. Startup applies that file, including `ADD COLUMN IF NOT EXISTS`, so a restart migrates the local database. There is no separate migration runner.

### 2026-10 — Behavior tests share one database and package-level limits

**Assumed:** API tests are isolated and safe to run with `t.Parallel()`.
**Actually:** `newBehaviorServer` sets `DB_NAME=cronsentry_test`, creates that database, and `TRUNCATE`s `users` (cascade) and `stripe_events` on every test. Rate-limit tests mutate package variables (`pingPerIPLimit` and the others) and `NewServer` copies those values. Parallel tests steal each other's rows and limits.
**Do this:** Do not mark these tests parallel. Restore limits with the helper's defer. Point the dev API at `cronsentry`, never at `cronsentry_test`, or a test run will wipe the accounts you are clicking through.

## GitHub and pull requests

### 2026-10 — `master` is the only integration branch

**Assumed:** An open feature branch was a fine base for the next pull request, and `main` might exist.
**Actually:** There is no `main`. Stacking pull requests on unmerged feature branches mixed already-landed work into the diff and hid the real change. Review asked for every PR to target `master`.
**Do this:** Branch from `origin/master`. Open the PR against `master`. Do not retarget, undraft, or merge unless the person who owns the repo asks.

### 2026-10 — A push can fail while GitHub returns HTTP 200

**Assumed:** A non-zero exit or a 4xx is the only failed push.
**Actually:** `git-receive-pack` sometimes answers HTTP 200 and then a sideband `Internal Server Error`. The branch does not move. Writing the commit through the Contents API is worse: it skips the normal receive path and is easy to get wrong.
**Do this:** Retry `git push -u origin <branch>` with backoff (4s, 8s, 16s, 32s). Confirm the remote tip moved. Commit from a normal git checkout.

### 2026-10 — Several GitHub writes are forbidden for the integration

**Assumed:** The agent that opens issues can also close them, set branch protection, and read the authenticated user.
**Actually:** Closing issue #5, setting branch protection, and `GET /user` return 403. The public profile for the repo owner has no published email.
**Do this:** Do not invent a security contact. Private reports go through GitHub security advisories, which is what `SECURITY.md` says. Leave issues you cannot close alone.

## API behavior

### 2026-10 — Logout behind auth never clears the cookie

**Assumed:** `POST /api/logout` should require a valid session, like the other account routes.
**Actually:** An expired or garbage session dies in the auth middleware with 401. The handler never runs, so the `HttpOnly` `opensentry_session` cookie stays. The browser keeps sending it and every later call 401s.
**Do this:** Logout is public. It returns 204 and `Set-Cookie` with `Max-Age=0` even when no session matches. The dashboard calls logout when a request returns 401.

### 2026-10 — Another user's job is 404, not 401

**Assumed:** A management call for a job id that exists but belongs to someone else should be 401 or 403, so the client can tell "forbidden" from "missing".
**Actually:** `GetJobForUser` requires a workspace membership, not a matching `jobs.user_id`. A miss and a job in a workspace the caller does not belong to are the same result: 404 `Job not found`. Returning 401 after a row is found tells the caller the id exists. The job list is the caller's active workspace only. Ping is the exception: `POST /api/ping/{token}` does not check the session. Anyone with the ping token gets 200, including for another workspace's job.
**Do this:** Keep management routes on 404 for ids outside the caller's workspaces. Do not add an existence check that answers differently. A teammate in the same workspace can read, edit, delete, and rotate the job. Treat the ping URL as a password. Billing and alert channels stay on the user who created the job.

### 2026-10 — Job updates are partial, and clients cannot mark a job missing

**Assumed:** `PUT /api/jobs/{id}` replaces the job, and any status string in the model (`healthy`, `missing`, `paused`) is a legal client write.
**Actually:** The body uses pointers. An omitted field stays as it is. A present `description: ""` clears the description. The only client statuses are `healthy` and `paused`. `missing` is 400. The checker is what sets `missing`, after `next_expect` plus grace minutes. Resume from `paused` recomputes `next_expect`. Pause keeps `next_expect` and `last_ping`. A success ping while paused updates those times and stays paused. It does not alert.
**Do this:** Preserve omit-versus-empty. Do not let a client write `missing`. Do not alert on a paused job.

### 2026-10 — New jobs are not pinged by their id

**Assumed:** `POST /api/ping/{id}` is the heartbeat, because the job id is in the URL the UI used to show.
**Actually:** New jobs get a `ping_token` that is a different UUID from `id`. Pinging the id returns 404 `Job not found`. Jobs created before tokens existed were backfilled with `ping_token = id` until someone rotates. Rotate invalidates the old URL immediately and does not change the job id.
**Do this:** Build ping URLs from `ping_token`. Assert in tests that a new token is not the id.

### 2026-10 — Ping suffixes must share the token's rate-limit bucket

**Assumed:** The per-URL limiter can key on everything after `/api/ping/`.
**Actually:** That key is `token/start` for a start signal and `token` for the success ping. They do not share the 30-per-minute token budget, so a caller can multiply the limit by adding suffixes. Start and fail are still pings.
**Do this:** Key the token bucket on the first path segment only. `/start` and `/fail` also count toward the 60-per-minute IP limit. `/healthz`, `OPTIONS`, and `POST /api/stripe/webhook` are not limited. A rejected attempt is not stored again. If the IP limit fails, do not also increment the token bucket. Limits are in memory, per process, and reset on restart. They are not shared across replicas.

### 2026-10 — The Stripe webhook sat on the dashboard's rate limit

**Assumed:** Anything under `/api/` that is not login or a ping should use the 120-per-minute management limit.
**Actually:** Stripe retries a burst from one egress IP. Those `POST /api/stripe/webhook` calls returned 429 and consumed the same budget as the signed-in dashboard. A retry storm locked the user out of their own checks.
**Do this:** Exempt the webhook the same way as `/healthz`. Signature verification still applies. A missing billing config returns 404, not 429.

### 2026-10 — A half-set Stripe config must not limit self-host

**Assumed:** Setting `STRIPE_SECRET_KEY` alone should turn on Free-plan caps, so hosted and self-host share one code path.
**Actually:** Self-host installs were one variable away from a 5-check cap they did not ask for. Hosted billing is on only when `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, and `STRIPE_PRICE_ID` are all set. Anything less logs and stays unlimited.
**Do this:** Keep that gate. Free is 5 checks and a 5-minute schedule gap. Pro is 50 checks and a 1-minute gap, only while the subscription is `active` or `trialing` and the price id matches. `past_due` and `canceled` use Free. Over-cap create is 402. A too-fast schedule is 400.

### 2026-10 — "How soon is the next tick" is not the schedule gap

**Assumed:** The minimum schedule interval is the time from now until `gronx`'s next tick. An hourly job (`0 * * * *`) is always at least an hour apart, so it passes the Free plan's 5-minute floor.
**Actually:** At 12:00:30, the next tick of an every-minute cron is 30 seconds away, so the check returned 400 "at least 5 minutes apart" for legal schedules. The same bug made the hourly test fail.
**Do this:** Measure the gap between consecutive ticks. Start at the next exclusive tick, then measure the following gaps. Apply the floor when a check is created or its schedule changes. A check that is already faster than the plan allows keeps running. It is not paused.

### 2026-10 — Webhook tests passed once, then failed on replay

**Assumed:** `TRUNCATE users CASCADE` resets billing, so a second `go test` starts clean.
**Actually:** `stripe_events` is not a child of `users`. The first test inserts `evt_new`. The next test sees the same id, treats it as a duplicate, and does not apply the subscription. The assertion that the account became Pro fails only on the second run.
**Do this:** Truncate `stripe_events` in the behavior-test setup. Keep the handler's contract: store the event id, delete that id if handling fails so Stripe can retry, and ignore an older `event_created_at` after a newer one. `checkout.session.completed` only binds the customer. It does not change the plan. There is no Stripe SDK. `STRIPE_API_BASE` exists so tests can point at a fake server. Leave it unset outside tests. The signature is `t=<unix>,v1=<hex HMAC-SHA256 of "{t}.{raw body}">`. Other versions, including `v0`, are ignored. The timestamp must be within five minutes.

### 2026-10 — Comparing a create response time with a later GET

**Assumed:** `last_ping` and `next_expect` from the create response stay `time.Equal` to a later GET when the handler did not change them.
**Actually:** Create returns the `time.Time` value it inserted, nanoseconds included. Postgres stores microseconds. The later GET is the stored value. `Equal` fails and the test reports that a start signal moved the schedule.
**Do this:** GET the job, perform the action, GET again, and compare those two reads.

### 2026-10 — CORS `*` and a cross-origin dashboard

**Assumed:** `Access-Control-Allow-Origin: *` plus the session cookie is fine for a separate dashboard host.
**Actually:** Browsers will not send credentialed cookies to `*`. An empty `CORS_ORIGINS` sends no allow-origin header, which is what same-origin Vite and nginx need. A `*` entry, an entry without `://`, or an entry containing `*` is ignored. The Vite proxy does not need `CORS_ORIGINS`. Cron pings send no `Origin` and must not be blocked on that header.
**Do this:** List exact dashboard origins only when the browser talks to the API on another host. The dashboard sends `credentials: 'include'` and does not store the JSON bearer token. The cookie is `HttpOnly` and `SameSite=Lax`. `Secure` is set only for TLS or `X-Forwarded-Proto: https`.

### 2026-10 — nginx turned `/healthz` into the SPA

**Assumed:** Proxying `/api/` is enough, and `$scheme` is the right value for `X-Forwarded-Proto`.
**Actually:** The SPA `try_files` fallback answered `/healthz` with `index.html`, so the probe looked healthy when Postgres was down. `$scheme` inside the container is `http` even when the browser used HTTPS, so the session cookie was not marked `Secure`. Without `mime.types`, the built JavaScript was served as `text/plain` and the dashboard stayed blank.
**Do this:** Give `/healthz` its own proxy location. Forward the incoming `X-Forwarded-Proto` (`$http_x_forwarded_proto`), not `$scheme`. Include the nginx mime types. `/healthz` returns 200 `{"status":"ok"}` only when the database ping works, and 503 when it does not. It is public and not rate limited.

### 2026-10 — Demo seed does not reset an existing password

**Assumed:** `DEMO_SEED=true` makes `test@example.com` / `opensentry-demo` work on every boot.
**Actually:** The seed inserts that user only when the email is missing. An older database still has a bcrypt hash of `secret`. The documented password then returns 401 `Invalid email or password`.
**Do this:** Say so in the docs when you touch seed behavior. Unknown email and a bad password must stay the same 401 string. Do not reveal which emails exist.

## Dashboard

### 2026-10 — The open job is React state, not the URL

**Assumed:** Reloading the browser after a ping refreshes the detail page that is on screen.
**Actually:** The selected job id lives in component state. The address stays `/`. A reload returns to the list. The detail page also does not poll, so a curl from a terminal does not change the numbers until the job is opened again.
**Do this:** After a ping, go back to the list and open the job. Do not write a test or a demo that reloads `/` and expects the detail view. Creating a job appends it to the end of the in-memory list. A fresh `GET /api/jobs` is `created_at DESC`, so the same job is first after a reload. Click the card you mean, not "the first Details button" across both states.

### 2026-10 — A new job's last ping is not empty

**Assumed:** `last_ping` is null until the first heartbeat, and the UI shows "Never".
**Actually:** Create stores `last_ping` as the creation time and computes `next_expect` from the cron. The zero-time "Never" path is for a missing timestamp, not for a new job.
**Do this:** Do not special-case a brand-new job as never pinged. Format a real timestamp.

## Still broken

### 2026-10-13 — Dry-run email never leaves `pending`

**Assumed:** With `SENDGRID_API_KEY` unset, the processor logs the email and marks the notification skipped.
**Actually:** `markNotificationStatus` uses `$1` both as the new `status` and in `CASE WHEN $1 = 'sent'`. Postgres rejects it with `pq: inconsistent types deduced for parameter $1`. The dry-run path logs "would send email" and then "Error marking notification as skipped". The row stays `pending`, so the same alert is attempted on every tick. `processor_test.go` only asserts that an unreachable database returns an error containing "skipped", so CI stays green.
**Do this:** When you next touch notifications, give the status comparison its own parameter or cast so skipped, sent, and failed all update. Assert the row status against a real Postgres, not only an unreachable one. Until that lands, a local API with the default dry-run sender will retry old alerts forever and fill the log.
