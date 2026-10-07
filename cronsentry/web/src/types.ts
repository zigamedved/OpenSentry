export interface Job {
  id: string;
  name: string;
  description: string;
  schedule: string;
  grace_time: number;
  status: 'healthy' | 'missing' | 'paused';
  last_ping: string;
  next_expect: string;
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

export function formatTime(value: string | undefined) {
  if (!value || value.startsWith('0001-01-01')) return 'Never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Never';
  return date.toLocaleString();
}
