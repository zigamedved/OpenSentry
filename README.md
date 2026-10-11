# OpenSentry

[![CI](https://github.com/zigamedved/OpenSentry/actions/workflows/ci.yml/badge.svg)](https://github.com/zigamedved/OpenSentry/actions/workflows/ci.yml)

Project is still a prototype and not production ready yet, so keep that in mind.

OpenSentry is a lightweight, reliable monitoring service for your cron jobs and scheduled tasks. Get notified immediately when your scheduled jobs fail to run on time.

The Go module and Docker app live in [`cronsentry/`](cronsentry/). That directory name is the original package path and is kept so existing clones and Compose files keep working. The product name is OpenSentry.

## Features

- **Simple Ping System**: Just add a simple curl command to your cron job
- **Flexible Alert Thresholds**: Set custom grace periods for each job
- **Email Notifications**: Get notified when jobs fail to run (SendGrid when configured; otherwise dry-run)
- **Status Dashboard**: View the health of all your jobs in one place
- **Slack and Discord**: Incoming webhooks for miss and recovery alerts
- **Accounts**: Email and password. Each account sees only its own jobs.

## Dashboard

![alt text](./assets/dashboard.png)

### Ping System Architecture

1. **User-side Integration**:
   - Register a job in OpenSentry to get a ping URL
   - Add an HTTP POST to the end of your cron job command:
     ```
     curl -s -X POST http://your-opensentry-host:8080/api/ping/YOUR_PING_TOKEN
     ```
   - This curl command sends a "heartbeat" to OpenSentry after your job completes successfully
   - Treat the ping URL like a password. Do not commit it, paste it in chat, or put it in a screenshot. Anyone who has it can record a ping. Rotate it from the job page when it leaks. The old URL stops working immediately. The job id in the dashboard does not change.

2. **Server-side Monitoring**:
   - When a ping is received, OpenSentry updates the job's status to "healthy"
   - A background service runs every 10 seconds to check for missing jobs
   - If a job misses its expected ping time plus its grace period, its status changes to "missing"
   - Missing jobs trigger notifications based on your settings

3. **Job Status Lifecycle**:
   - **Healthy**: Job is running on schedule
   - **Missing**: No ping received when expected
   - **Paused**: Monitoring temporarily disabled

This design is lightweight and effective because it requires no agent installation on your servers — just a curl command added to your existing cron jobs.

## API Usage

### Create a Job

Sign in with email and password. `POST /api/register` and `POST /api/login` return a session token and set an `opensentry_session` cookie. Send that token as `Authorization: Bearer` on management routes, or rely on the cookie from the dashboard. `POST /api/logout` ends the session. `GET /api/me` returns the signed-in user. Ping routes do not require a session.

```bash
curl -c cookies.txt -X POST http://localhost:8080/api/login \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"your-password"}'

curl -X POST http://localhost:8080/api/jobs \
  -b cookies.txt \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Database Backup",
    "description": "Daily database backup job",
    "schedule": "0 0 * * *",
    "grace_time": 15
  }'
```

### Ping a Job

Pings are HTTP **POST** requests. A GET does not record a heartbeat.

```bash
curl -X POST http://localhost:8080/api/ping/YOUR_PING_TOKEN
```

`YOUR_PING_TOKEN` is the job's `ping_token`, not its id. A new job gets a token that is different from the id. Jobs created before tokens existed keep their old URL until you rotate: that token starts out equal to the job id.

Rotate from the job page, or:

```bash
curl -X POST http://localhost:8080/api/jobs/JOB_ID/rotate-ping \
  -b cookies.txt
```

The response is the job with the new `ping_token`. The previous ping URL then returns 404.

Pings and account routes are rate limited in memory on each API process. Limits reset when the process restarts and are not shared across replicas.

| Route | Limit |
| --- | --- |
| `POST /api/ping/…` | 60 per minute per IP, and 30 per minute per ping URL |
| `POST /api/login`, `POST /api/register` | 20 per minute per IP |
| Other `/api` routes | 120 per minute per IP |
| `GET /healthz`, `OPTIONS` | not limited |

A limited request returns 429 `Too Many Requests` and `Retry-After: 60`. The client IP is the first `X-Forwarded-For` value, or the connection address when that header is absent. Compose binds the API to localhost and nginx sets the header. A client who can reach the API directly can set the header itself.

## Quick Start

### Using Docker Compose

1. Clone the repository:

   ```
   git clone https://github.com/zigamedved/OpenSentry.git
   cd OpenSentry/cronsentry
   ```

2. Start the application:

   ```
   docker compose up -d --build
   ```

3. Open the dashboard at http://localhost:3000 (the API listens on http://localhost:8080) and create an account. Each account sees only its own jobs. Each job card shows a copyable `POST` curl. That ping URL is a secret, like a password. Do not commit it. Rotate it from the job page when it leaks; the old URL stops working immediately.

4. Run the copied ping (or the example below). Refresh the dashboard. The job's last ping time updates and the status stays healthy.

The Compose UI is nginx on port 3000. It proxies `/api` to the API container, and the frontend calls that same-origin path. `VITE_API_URL` is a **build** argument (Vite inlines it). Leave it empty.

The dashboard signs in with the `opensentry_session` cookie. Nginx in Compose and the Vite dev server proxy `/api`, so the browser treats those calls as same-origin and sends the cookie without CORS. Leave `VITE_API_URL` empty.

`CORS_ORIGINS` is an optional comma-separated list of exact browser origins, such as `https://monitor.example.com`. The API echoes `Access-Control-Allow-Origin` only for a listed origin and allows credentials, so a dashboard on another host can send the cookie. Leave it unset when the dashboard and the API share an origin. A `*` entry is ignored. Cron pings send no `Origin` header and do not need to be listed. See [SECURITY.md](SECURITY.md) for how the cookie and the bearer token differ if the dashboard has XSS.

### Local Go and Vite

See [CONTRIBUTING.md](CONTRIBUTING.md). The dev dashboard is http://localhost:5173 and proxies `/api` to http://localhost:8080.

## Configuration

The API reads these environment variables. Compose sets the database values shown below.

| Variable | Default | Purpose |
| --- | --- | --- |
| `DB_HOST` | `localhost` | Postgres host |
| `DB_PORT` | `5432` | Postgres port |
| `DB_USER` | `postgres` | Postgres user |
| `DB_PASSWORD` | `postgres` | Postgres password |
| `DB_NAME` | `cronsentry` | Database name |
| `DB_SSLMODE` | `disable` | lib/pq SSL mode. `disable` for the Compose Postgres container. `require` (or `verify-full` with a CA) for a database reached over the internet. |
| `PORT` | `8080` | API listen port |
| `DEMO_SEED` | unset | When `true`, create `test@example.com` / `opensentry-demo` if that email is missing, and attach the Slack and Discord env webhooks to that user. |
| `SENDGRID_API_KEY` | unset | When set, missing-job email is sent through SendGrid. When unset, the API logs `would send` and marks the notification `skipped`. |
| `EMAIL_FROM` | `OpenSentry <noreply@localhost>` | SendGrid from address. Use a verified sender, for example `OpenSentry <alerts@yourdomain>`. |
| `DASHBOARD_URL` | unset | Link included in alerts. Compose defaults this to `http://localhost:3000`. |
| `CORS_ORIGINS` | unset | Comma-separated browser origins allowed to read API responses, for example `https://monitor.example.com`. Unset keeps the API same-origin only. `*` is ignored. |
| `SLACK_WEBHOOK_URL` | unset | Slack incoming webhook for the self-host user. Saved on startup when set. |
| `DISCORD_WEBHOOK_URL` | unset | Discord webhook for the self-host user. Saved on startup when set. |

`GET /healthz` returns 200 `{"status":"ok"}` when the API can ping Postgres, and 503 when it cannot. It does not require a session. The nginx dashboard proxies `/healthz` to the API so an orchestrator can probe the public site.

Accounts use email and password. Passwords are stored as bcrypt hashes and are never returned in JSON. A session is an opaque token stored only as a SHA-256 hash, valid for 14 days. The dashboard uses the `opensentry_session` cookie (`HttpOnly`, `SameSite=Lax`, `Secure` on HTTPS). Page JavaScript cannot read that cookie. XSS on the dashboard can still call the API as the signed-in user, but it cannot copy the cookie to another site. Login and register also return `token` for `Authorization: Bearer`. The dashboard does not store it. If a script keeps that token in JavaScript or local storage, XSS can steal it and replay it from anywhere. Prefer the cookie in browsers. There is no shared `API_TOKEN`. `POST /api/logout` does not require a live session: it deletes the token when one matches and always expires the cookie.

Jobs created before accounts belong to `test-user` (`test@example.com`) and stay hidden from accounts you register later. Set `DEMO_SEED=true` and restart once to sign in as that user, or create a new account and new jobs. If `test@example.com` is already in the database, `DEMO_SEED` leaves its password unchanged (the old seed password was `secret`). A missing email is created as `opensentry-demo`.

Compose does not configure an email provider. A missing `SENDGRID_API_KEY` is safe: alerts are logged and marked skipped, not failed. Create a SendGrid API key, verify the `EMAIL_FROM` sender, and set `SENDGRID_API_KEY` when you want real mail.

## Slack and Discord

Miss and recovery alerts go to every channel configured for the user: email, plus Slack and Discord when a webhook is saved. A delivery failure is stored on that notification and does not stop the checker or the other channels.

Paste the webhook in the dashboard (Alert channels) while signed in. `SLACK_WEBHOOK_URL` / `DISCORD_WEBHOOK_URL` are stored for the demo user only when `DEMO_SEED=true`. An empty env var does not erase a URL saved in the dashboard. `PUT /api/channels/slack` or `PUT /api/channels/discord` with `{"webhook_url":""}` clears a channel. These routes require a session.

Create a Slack incoming webhook in your workspace's app settings, or a Discord channel webhook (Integrations → Webhooks → New Webhook). OpenSentry posts JSON:

Slack:

```json
{"text":"OpenSentry: Job 'Nightly backup' has missed its scheduled run time\nJob: Nightly backup\nWhen: Tue, 07 Oct 2026 12:00:00 UTC\nDashboard: https://monitor.example"}
```

Discord:

```json
{"content":"OpenSentry: Job 'Nightly backup' has missed its scheduled run time\nJob: Nightly backup\nWhen: Tue, 07 Oct 2026 12:00:00 UTC\nDashboard: https://monitor.example"}
```

## Production on a VPS

One VM, a domain, and Docker. Caddy terminates HTTPS and proxies to the nginx dashboard. nginx serves the built UI and proxies `/api/` and `/healthz` to the Go API. Postgres stays on the Compose network. Its published port, and the API and dashboard ports, bind to `127.0.0.1` only.

1. Point DNS for your domain at the VM and open TCP 80 and 443.
2. On the VM:

```bash
git clone https://github.com/zigamedved/OpenSentry.git
cd OpenSentry/cronsentry
export SITE_ADDRESS=monitor.example.com
export DASHBOARD_URL=https://monitor.example.com
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

Compose reads those variables from the environment or from a `.env` file in `cronsentry/`. Do not commit `.env`. `POSTGRES_PASSWORD` is the password for both the database container and the API. Leave `SENDGRID_API_KEY` unset to keep email in dry-run, or set it with a verified `EMAIL_FROM`.

3. Open `https://monitor.example.com`, create an account, and copy a ping command from a job card. `GET https://monitor.example.com/healthz` is the probe: 200 when Postgres answers, 503 when it does not.

Caddy gets a Let's Encrypt certificate for `SITE_ADDRESS` and sends `X-Forwarded-Proto: https`. nginx forwards that header, and the API marks the session cookie `Secure`. nginx also appends the visitor to `X-Forwarded-For`. Rate limits use that address. They live in each API process, so a second replica would count separately.

The public site is one origin, so leave `CORS_ORIGINS` unset. Set it only when a browser on a different origin calls the API.

The bundled Postgres image does not speak TLS. Leave `DB_SSLMODE=disable` for it. The connection stays on the Docker network.

For a hosted Postgres service, set `DB_HOST` to that host and `DB_SSLMODE=require` so the session is encrypted. Use `verify-full` when the provider supplies a CA (`sslrootcert`). `require` encrypts without checking the server certificate. Do not use `disable` for a database reached over the internet. `verify-ca` checks the CA and not the hostname.

Local Compose is unchanged aside from listening on localhost: `http://localhost:3000` for the dashboard and `http://localhost:8080` for the API. The database password defaults to `postgres` when `POSTGRES_PASSWORD` is unset.

## License

OpenSentry is released under the [MIT License](LICENSE). You may use, modify, and ship it, including as a commercial hosted service, as long as the copyright notice and this license are included with the software.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).
