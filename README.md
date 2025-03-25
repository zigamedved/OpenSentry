# CronSentry

CronSentry is a lightweight monitoring service for your cron jobs and scheduled tasks. Get notified immediately when your scheduled jobs fail to run on time.

## Features

- **Simple Ping System**: Just add a simple curl command to your cron job
- **Flexible Alert Thresholds**: Set custom grace periods for each job
- **Email Notifications**: Get notified when jobs fail to run
- **Status Dashboard**: View the health of all your jobs in one place
- **Timezone Support**: Set different timezones for each monitored job
- **Extensible**: Easy to add Slack, Discord, or other notification methods

## How It Works

### Ping System Architecture

1. **User-side Integration**:
   - Register a job in CronSentry to get a unique job ID
   - Add a simple HTTP request to the end of your cron job command:
     ```
     * * * * * /path/to/your/script.sh && curl -s http://your-cronsentry-host:8080/api/ping/YOUR_JOB_ID > /dev/null
     ```
   - This curl command sends a "heartbeat" to CronSentry after your job completes successfully

2. **Server-side Monitoring**:
   - When a ping is received, CronSentry updates the job's status to "healthy"
   - A background service runs every 10 seconds to check for missing jobs
   - If a job misses its expected ping time, its status changes to "missing"
   - Missing jobs trigger notifications based on your settings

3. **Job Status Lifecycle**:
   - **Healthy**: Job is running on schedule
   - **Late**: Job pinged but outside grace period
   - **Missing**: No ping received when expected
   - **Paused**: Monitoring temporarily disabled

This design is lightweight and effective because it requires no agent installation on your servers - just a simple curl command added to your existing cron jobs.

## Quick Start

### Using Docker Compose

1. Clone the repository:
   ```
   git clone https://github.com/zigamedved/cronsentry.git
   cd cronsentry
   ```

2. Start the application:
   ```
   docker-compose up -d
   ```

3. Access the dashboard at http://localhost:8080

### Using Existing Installation

1. Add a ping to your cron job by adding this at the end of your command:
   ```
   curl -s http://your-cronsentry-host:8080/api/ping/YOUR_JOB_ID > /dev/null
   ```

## API Usage

### Create a Job

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Database Backup",
    "description": "Daily database backup job",
    "schedule": "0 0 * * *",
    "timezone": "UTC",
    "grace_time": 15
  }'
```

### Ping a Job

```bash
curl -X POST http://localhost:8080/api/ping/YOUR_JOB_ID
```

## Pricing Plans

- **Free Tier**: Monitor up to 3 jobs, email notifications
- **Personal Plan**: $9/month - Monitor up to 20 jobs, email notifications
- **Team Plan**: $29/month - Monitor up to 100 jobs, email + Slack notifications
- **Business Plan**: $99/month - Unlimited jobs, all notifications, priority support

## Development

### Prerequisites

- Go 1.22 or higher
- PostgreSQL 13 or higher

### Local Setup

1. Clone the repository:
   ```
   git clone https://github.com/zigamedved/cronsentry.git
   cd cronsentry
   ```

2. Install dependencies:
   ```
   go mod download
   ```

3. Set up environment variables:
   ```
   export DB_HOST=localhost
   export DB_PORT=5432
   export DB_USER=postgres
   export DB_PASSWORD=postgres
   export DB_NAME=cronsentry
   ```

4. Run the application:
   ```
   go run ./cmd
   ```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details. 