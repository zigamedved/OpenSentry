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
- **Extensible**: Easy to add Slack, Discord, or other notification methods // In progress
- **Authentication**: Authentication via TBD // In progress

## Dashboard

![alt text](./assets/dashboard.png)

### Ping System Architecture

1. **User-side Integration**:
   - Register a job in OpenSentry to get a unique job ID
   - Add an HTTP POST to the end of your cron job command:
     ```
     curl -s -X POST http://your-opensentry-host:8080/api/ping/YOUR_JOB_ID
     ```
   - This curl command sends a "heartbeat" to OpenSentry after your job completes successfully
   - Treat the job ID as a secret. Anyone who has it can record a ping.

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

Management routes require `API_TOKEN`. Ping routes do not.

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Authorization: Bearer $API_TOKEN" \
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
curl -X POST http://localhost:8080/api/ping/YOUR_JOB_ID
```

## Quick Start

### Using Docker Compose

1. Clone the repository:

   ```
   git clone https://github.com/zigamedved/OpenSentry.git
   cd OpenSentry/cronsentry
   ```

2. Generate a management token and start the application. There is no default token:

   ```
   export API_TOKEN="$(openssl rand -hex 32)"
   docker compose up -d --build
   ```

3. Open the dashboard at http://localhost:3000 (the API listens on http://localhost:8080) and paste `API_TOKEN` into the token field. Each job card shows a copyable `POST` curl. The job id in that URL is a secret.

4. Run the copied ping (or the example below). Refresh the dashboard. The job's last ping time updates and the status stays healthy.

The Compose UI is nginx on port 3000. It proxies `/api` to the API container, and the frontend calls that same-origin path. `VITE_API_URL` is a **build** argument (Vite inlines it). Leave it empty so the browser uses relative `/api` URLs. Set it only when the API is on a different origin:

```
docker compose build --build-arg VITE_API_URL=https://api.example.com web
```

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
| `DB_SSLMODE` | `disable` | lib/pq SSL mode. Use `require` for hosted Postgres. |
| `PORT` | `8080` | API listen port |
| `API_TOKEN` | unset | Bearer token for `/api/jobs`. When unset, those routes return 401. `POST /api/ping/{id}` stays public. |
| `SENDGRID_API_KEY` | unset | When set, missing-job email is sent through SendGrid. When unset, the API logs `would send` and marks the notification `skipped`. |
| `EMAIL_FROM` | `OpenSentry <noreply@localhost>` | SendGrid from address. Use a verified sender, for example `OpenSentry <alerts@yourdomain>`. |
| `DASHBOARD_URL` | unset | Link included in alert email. Compose defaults this to `http://localhost:3000`. |

`GET /healthz` returns 200 when the API can ping Postgres. It does not require `API_TOKEN`.

Compose does not configure an email provider. A missing `SENDGRID_API_KEY` is safe: alerts are logged and marked skipped, not failed. Create a SendGrid API key, verify the `EMAIL_FROM` sender, and set `SENDGRID_API_KEY` when you want real mail.

## License

OpenSentry is released under the [MIT License](LICENSE). You may use, modify, and ship it, including as a commercial hosted service, as long as the copyright notice and this license are included with the software.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).
