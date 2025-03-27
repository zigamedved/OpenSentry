# CronSentry

Project is still a prototype and not production ready yet, so keep that in mind.

CronSentry is a lightweight, reliable monitoring service for your cron jobs and scheduled tasks. Get notified immediately when your scheduled jobs fail to run on time.

## Features

- **Simple Ping System**: Just add a simple curl command to your cron job
- **Flexible Alert Thresholds**: Set custom grace periods for each job
- **Email Notifications**: Get notified when jobs fail to run // In progress
- **Status Dashboard**: View the health of all your jobs in one place // In progress
- **Extensible**: Easy to add Slack, Discord, or other notification methods // In progress
- **Authentication**: Authentication via TBD // In progress

### Ping System Architecture

1. **User-side Integration**:
   - Register a job in CronSentry to get a unique job ID
   - Add a simple HTTP request to the end of your cron job command:
     ```
     curl -s http://your-cronsentry-host:8080/api/ping/YOUR_JOB_ID
     ```
   - This curl command sends a "heartbeat" to CronSentry after your job completes successfully

2. **Server-side Monitoring**:
   - When a ping is received, CronSentry updates the job's status to "healthy"
   - A background service runs every 10 seconds to check for missing jobs
   - If a job misses its expected ping time + grace period, its status changes to "missing"
   - Missing jobs trigger notifications based on your settings

3. **Job Status Lifecycle**:
   - **Healthy**: Job is running on schedule
   - **Missing**: No ping received when expected
   - **Paused**: Monitoring temporarily disabled

This design is lightweight and effective because it requires no agent installation on your servers - just a simple curl command added to your existing cron jobs.

## API Usage

### Create a Job

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Database Backup",
    "description": "Daily database backup job",
    "schedule": "0 0 * * *",
    "grace_time": 15
  }'
```

### Ping a Job

```bash
curl -X POST http://localhost:8080/api/ping/YOUR_JOB_ID
```

## Quick Start

### Using Docker Compose

1. Clone the repository:
   ```
   git clone https://github.com/yourusername/cronsentry.git
   cd cronsentry
   ```

2. Start the application:
   ```
   docker-compose up -d
   ```

3. Access the dashboard at http://localhost:8080 // Feature in progress

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
