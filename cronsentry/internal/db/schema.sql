CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
 
CREATE TABLE IF NOT EXISTS jobs (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    schedule VARCHAR(255) NOT NULL,
    grace_time INTEGER NOT NULL DEFAULT 10,
    last_ping TIMESTAMPTZ,
    next_expect TIMESTAMPTZ,
    status VARCHAR(50) NOT NULL DEFAULT 'healthy',
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ping_token VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS job_events (
    id VARCHAR(36) PRIMARY KEY,
    job_id VARCHAR(36) NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL,
    data JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS notifications (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id VARCHAR(36) REFERENCES jobs(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending', 
    sent_at TIMESTAMPTZ,
    data JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL
);

-- Existing installs created notifications without data; add it if missing.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS data JSONB DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id);

-- Jobs created before ping tokens used the job id as the ping URL.
-- Keep that URL until the owner rotates it.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS ping_token VARCHAR(64);
UPDATE jobs SET ping_token = id WHERE ping_token IS NULL OR ping_token = '';
ALTER TABLE jobs ALTER COLUMN ping_token SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_ping_token ON jobs (ping_token);

-- Start/fail signals. started_at is set by /start and cleared by success or /fail.
-- last_body is the newest non-empty ping output, capped by the API.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS last_duration_ms BIGINT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS last_body TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_job_events_job_id ON job_events(job_id);
CREATE INDEX IF NOT EXISTS idx_job_events_created_at ON job_events(created_at);
CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_job_id ON notifications(job_id);

CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- Hosted billing. No row means the Free plan when Stripe is configured.
-- Self-host installs leave Stripe unset and do not enforce these rows.
CREATE TABLE IF NOT EXISTS billing_accounts (
    user_id VARCHAR(36) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan VARCHAR(20) NOT NULL DEFAULT 'free',
    stripe_customer_id VARCHAR(255),
    stripe_subscription_id VARCHAR(255),
    subscription_status VARCHAR(40) NOT NULL DEFAULT '',
    event_created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_stripe_customer
    ON billing_accounts (stripe_customer_id)
    WHERE stripe_customer_id IS NOT NULL AND stripe_customer_id <> '';

CREATE TABLE IF NOT EXISTS stripe_events (
    id VARCHAR(255) PRIMARY KEY,
    received_at TIMESTAMPTZ NOT NULL
);

-- Workspaces. Existing users get one personal workspace whose id matches
-- the user id, and their jobs move to it. New users get a workspace from
-- CreateUser. Jobs stay visible to every member of that workspace.
CREATE TABLE IF NOT EXISTS workspaces (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_members (
    workspace_id VARCHAR(36) NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE TABLE IF NOT EXISTS workspace_invites (
    id VARCHAR(36) PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL,
    token VARCHAR(64) NOT NULL UNIQUE,
    invited_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_workspace_members_user ON workspace_members (user_id);
CREATE INDEX IF NOT EXISTS idx_workspace_invites_email ON workspace_invites (email);

ALTER TABLE users ADD COLUMN IF NOT EXISTS active_workspace_id VARCHAR(36);

INSERT INTO workspaces (id, name, created_at, updated_at)
SELECT u.id, u.name || '''s workspace', u.created_at, u.updated_at
FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM workspace_members m WHERE m.user_id = u.id
);

INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
SELECT u.id, u.id, 'admin', u.created_at
FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM workspace_members m WHERE m.user_id = u.id
);

UPDATE users
SET active_workspace_id = id
WHERE active_workspace_id IS NULL
  AND EXISTS (
      SELECT 1 FROM workspace_members m
      WHERE m.user_id = users.id AND m.workspace_id = users.id
  );

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36);
UPDATE jobs SET workspace_id = user_id WHERE workspace_id IS NULL OR workspace_id = '';
ALTER TABLE jobs ALTER COLUMN workspace_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_workspace_id ON jobs (workspace_id);

DO $$
BEGIN
    ALTER TABLE jobs
        ADD CONSTRAINT jobs_workspace_id_fkey
        FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS alert_channels (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(20) NOT NULL,
    webhook_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, kind)
); 