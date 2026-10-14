export interface Job {
  id: string;
  name: string;
  description: string;
  schedule: string;
  grace_time: number;
  status: 'healthy' | 'missing' | 'paused';
  ping_token: string;
  last_ping: string;
  next_expect: string;
  started_at?: string;
  last_duration_ms?: number;
  last_body?: string;
  created_at?: string;
  updated_at?: string;
}

export interface JobEvent {
  id: string;
  job_id: string;
  type: string;
  data: string;
  created_at: string;
}

export function formatDuration(ms: number | undefined) {
  if (ms == null || Number.isNaN(ms)) return '—';
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remSeconds = seconds % 60;
  if (minutes < 60) return remSeconds ? `${minutes}m ${remSeconds}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return remMinutes ? `${hours}h ${remMinutes}m` : `${hours}h`;
}

export function formatTime(value: string | undefined) {
  if (!value || value.startsWith('0001-01-01')) return 'Never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Never';
  return date.toLocaleString();
}
