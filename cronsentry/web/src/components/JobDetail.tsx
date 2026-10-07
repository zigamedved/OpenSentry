import { useEffect, useState } from 'react';
import { PingCommand } from './JobCard';
import { formatTime, type Job, type JobEvent } from '../types';

interface JobDetailProps {
  jobId: string;
  apiUrl: string;
  token: string;
  apiOrigin: string;
  onBack: () => void;
  onDelete: (id: string) => Promise<string | null>;
  onAuthError: () => void;
}

function authHeaders(token: string, json = false): HeadersInit {
  const headers: Record<string, string> = {};
  if (json) headers['Content-Type'] = 'application/json';
  if (token) headers.Authorization = `Bearer ${token}`;
  return headers;
}

async function errorMessage(response: Response, fallback: string) {
  if (response.status === 401) return 'The API token was rejected.';
  const text = (await response.text()).trim();
  return text || fallback;
}

const statusColors: Record<string, string> = {
  healthy: 'bg-green-100 text-green-800',
  missing: 'bg-red-100 text-red-800',
  paused: 'bg-gray-100 text-gray-800',
};

export function JobDetail({ jobId, apiUrl, token, apiOrigin, onBack, onDelete, onAuthError }: JobDetailProps) {
  const [job, setJob] = useState<Job | null>(null);
  const [events, setEvents] = useState<JobEvent[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [eventsError, setEventsError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [schedule, setSchedule] = useState('');
  const [graceTime, setGraceTime] = useState(5);

  useEffect(() => {
    let cancelled = false;

    const run = async () => {
      setLoadError(null);
      setEventsError(null);
      try {
        const response = await fetch(`${apiUrl}/api/jobs/${jobId}`, { headers: authHeaders(token) });
        if (cancelled) return;
        if (response.status === 401) {
          onAuthError();
          setLoadError('The API token was rejected.');
          return;
        }
        if (response.status === 404) {
          setLoadError('Job not found.');
          setJob(null);
          return;
        }
        if (!response.ok) {
          setLoadError(await errorMessage(response, 'Failed to load job'));
          return;
        }
        const loaded = (await response.json()) as Job;
        if (cancelled) return;
        setJob(loaded);
        setName(loaded.name);
        setDescription(loaded.description || '');
        setSchedule(loaded.schedule);
        setGraceTime(loaded.grace_time);
      } catch {
        if (!cancelled) setLoadError('Failed to load job');
        return;
      }

      try {
        const response = await fetch(`${apiUrl}/api/jobs/${jobId}/events`, { headers: authHeaders(token) });
        if (cancelled) return;
        if (response.status === 401) {
          onAuthError();
          setEventsError('The API token was rejected.');
          return;
        }
        if (!response.ok) {
          setEventsError(await errorMessage(response, 'Failed to load events'));
          return;
        }
        setEvents((await response.json()) as JobEvent[]);
      } catch {
        if (!cancelled) setEventsError('Failed to load events');
      }
    };

    run();
    return () => {
      cancelled = true;
    };
  }, [apiUrl, jobId, onAuthError, token, reloadKey]);

  const saveStatus = async (status: 'paused' | 'healthy') => {
    setActionError(null);
    setSaving(true);
    try {
      const response = await fetch(`${apiUrl}/api/jobs/${jobId}`, {
        method: 'PUT',
        headers: authHeaders(token, true),
        body: JSON.stringify({ status }),
      });
      if (response.status === 401) {
        onAuthError();
        setActionError('The API token was rejected.');
        return;
      }
      if (!response.ok) {
        setActionError(await errorMessage(response, 'Failed to update status'));
        return;
      }
      setReloadKey((current) => current + 1);
    } catch {
      setActionError('Failed to update status');
    } finally {
      setSaving(false);
    }
  };

  const saveEdits = async (event: React.FormEvent) => {
    event.preventDefault();
    setActionError(null);
    setSaving(true);
    try {
      const response = await fetch(`${apiUrl}/api/jobs/${jobId}`, {
        method: 'PUT',
        headers: authHeaders(token, true),
        body: JSON.stringify({
          name,
          description,
          schedule,
          grace_time: graceTime,
        }),
      });
      if (response.status === 401) {
        onAuthError();
        setActionError('The API token was rejected.');
        return;
      }
      if (!response.ok) {
        setActionError(await errorMessage(response, 'Failed to save job'));
        return;
      }
      setReloadKey((current) => current + 1);
    } catch {
      setActionError('Failed to save job');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="rounded-lg bg-white p-6 shadow">
      <button type="button" onClick={onBack} className="text-sm font-medium text-blue-600 hover:text-blue-800">
        Back to jobs
      </button>

      {loadError ? (
        <div className="mt-6 text-center">
          <p className="text-sm text-red-600">{loadError}</p>
          <button
            type="button"
            onClick={() => setReloadKey((current) => current + 1)}
            className="mt-4 rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white"
          >
            Retry
          </button>
        </div>
      ) : null}

      {job ? (
        <div className="mt-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h2 className="text-2xl font-semibold text-gray-900">{job.name}</h2>
              <p className="mt-1 text-sm text-gray-500">{job.description || 'No description'}</p>
            </div>
            <span className={`w-fit rounded-full px-2.5 py-0.5 text-xs font-medium ${statusColors[job.status] ?? statusColors.paused}`}>
              {job.status}
            </span>
          </div>

          <dl className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm text-gray-500">Schedule</dt>
              <dd className="overflow-x-auto font-mono font-medium text-gray-900">{job.schedule}</dd>
            </div>
            <div>
              <dt className="text-sm text-gray-500">Grace</dt>
              <dd className="font-medium text-gray-900">{job.grace_time} minutes</dd>
            </div>
            <div>
              <dt className="text-sm text-gray-500">Last ping</dt>
              <dd className="font-medium text-gray-900">{formatTime(job.last_ping)}</dd>
            </div>
            <div>
              <dt className="text-sm text-gray-500">Next expected</dt>
              <dd className="font-medium text-gray-900">{formatTime(job.next_expect)}</dd>
            </div>
          </dl>

          <div className="mt-6">
            <PingCommand apiOrigin={apiOrigin} jobId={job.id} />
          </div>

          {actionError ? <p className="mt-4 text-sm text-red-600">{actionError}</p> : null}

          <div className="mt-4 flex flex-wrap gap-2">
            {job.status === 'paused' ? (
              <button
                type="button"
                disabled={saving}
                onClick={() => saveStatus('healthy')}
                className="rounded-md bg-blue-600 px-3 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
              >
                Resume
              </button>
            ) : (
              <button
                type="button"
                disabled={saving}
                onClick={() => saveStatus('paused')}
                className="rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white disabled:opacity-50"
              >
                Pause
              </button>
            )}
            <button
              type="button"
              disabled={saving}
              onClick={async () => {
                setActionError(null);
                const message = await onDelete(job.id);
                if (message) setActionError(message);
              }}
              className="rounded-md border border-red-200 px-3 py-2 text-sm font-semibold text-red-600 hover:bg-red-50 disabled:opacity-50"
            >
              Delete
            </button>
          </div>

          <form onSubmit={saveEdits} className="mt-8 space-y-4 border-t border-gray-100 pt-6">
            <h3 className="text-lg font-semibold text-gray-900">Edit job</h3>
            <div>
              <label htmlFor="edit-name" className="block text-sm font-medium text-gray-700">Name</label>
              <input
                id="edit-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
              />
            </div>
            <div>
              <label htmlFor="edit-description" className="block text-sm font-medium text-gray-700">Description</label>
              <textarea
                id="edit-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                rows={3}
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
              />
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label htmlFor="edit-schedule" className="block text-sm font-medium text-gray-700">Cron schedule</label>
                <input
                  id="edit-schedule"
                  value={schedule}
                  onChange={(event) => setSchedule(event.target.value)}
                  required
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
                />
              </div>
              <div>
                <label htmlFor="edit-grace" className="block text-sm font-medium text-gray-700">Grace time (minutes)</label>
                <input
                  id="edit-grace"
                  type="number"
                  min={1}
                  value={graceTime}
                  onChange={(event) => setGraceTime(Number(event.target.value))}
                  required
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
                />
              </div>
            </div>
            <button
              type="submit"
              disabled={saving}
              className="rounded-md bg-indigo-600 px-3 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              Save changes
            </button>
          </form>

          <section className="mt-8 border-t border-gray-100 pt-6">
            <h3 className="text-lg font-semibold text-gray-900">Events</h3>
            {eventsError ? <p className="mt-3 text-sm text-red-600">{eventsError}</p> : null}
            {events && events.length === 0 ? (
              <p className="mt-3 text-sm text-gray-500">No events yet. A ping, miss, or recovery will show up here.</p>
            ) : null}
            {events && events.length > 0 ? (
              <ol className="mt-4 space-y-3">
                {events.map((event) => (
                  <li key={event.id} className="flex items-center justify-between gap-3 rounded-md bg-gray-50 px-3 py-2">
                    <span className="text-sm font-medium text-gray-900">{event.type}</span>
                    <span className="text-sm text-gray-500">{formatTime(event.created_at)}</span>
                  </li>
                ))}
              </ol>
            ) : null}
          </section>
        </div>
      ) : null}
    </div>
  );
}
