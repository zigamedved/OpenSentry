import { useCallback, useEffect, useState } from 'react';
import { JobCard } from './components/JobCard';
import { JobDetail } from './components/JobDetail';
import { Stats } from './components/Stats';
import { NewJobModal } from './components/NewJobModal';
import { AlertChannels } from './components/AlertChannels';
import { PlusIcon } from '@heroicons/react/24/outline';
import type { Job } from './types';

// Empty string means same-origin /api (nginx in Compose, Vite proxy in dev).
const API_URL = import.meta.env.VITE_API_URL ?? '';
const TOKEN_KEY = 'opensentry.apiToken';

function authHeaders(token: string, json = false): HeadersInit {
  const headers: Record<string, string> = {};
  if (json) headers['Content-Type'] = 'application/json';
  if (token) headers.Authorization = `Bearer ${token}`;
  return headers;
}

function apiOrigin() {
  if (API_URL) return API_URL.replace(/\/$/, '');
  return window.location.origin;
}

async function readError(response: Response, fallback: string) {
  const text = (await response.text()).trim();
  return text || fallback;
}

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [modalKey, setModalKey] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [token, setToken] = useState(() => sessionStorage.getItem(TOKEN_KEY) || import.meta.env.VITE_API_TOKEN || '');
  const [tokenDraft, setTokenDraft] = useState(token);
  const [authError, setAuthError] = useState(false);

  const fetchJobs = useCallback(async (activeToken: string) => {
    try {
      const response = await fetch(`${API_URL}/api/jobs`, { headers: authHeaders(activeToken) });
      if (response.status === 401) {
        setAuthError(true);
        setLoadError(null);
        setJobs([]);
        return;
      }
      if (!response.ok) {
        setLoadError(await readError(response, 'Failed to load jobs'));
        return;
      }
      const data = await response.json();
      setAuthError(false);
      setLoadError(null);
      setJobs(data);
    } catch {
      setLoadError('Failed to load jobs');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchJobs(token);
  }, [fetchJobs, token]);

  const saveToken = (event: React.FormEvent) => {
    event.preventDefault();
    const next = tokenDraft.trim();
    sessionStorage.setItem(TOKEN_KEY, next);
    setToken(next);
    setIsLoading(true);
    fetchJobs(next);
  };

  const onAuthError = useCallback(() => {
    setAuthError(true);
  }, []);

  const stats = {
    total: jobs.length,
    healthy: jobs.filter(job => job.status === 'healthy').length,
    missing: jobs.filter(job => job.status === 'missing').length,
  };

  const handleDelete = async (id: string) => {
    try {
      const response = await fetch(`${API_URL}/api/jobs/${id}`, {
        method: 'DELETE',
        headers: authHeaders(token),
      });
      if (response.status === 401) {
        setAuthError(true);
        return 'The API token was rejected.';
      }
      if (!response.ok) return readError(response, 'Failed to delete job');
      setJobs((current) => current.filter(job => job.id !== id));
      if (selectedId === id) setSelectedId(null);
      return null;
    } catch {
      return 'Failed to delete job';
    }
  };

  const handleCreateJob = async (newJob: { name: string; description: string; schedule: string; grace_time: number }) => {
    try {
      const response = await fetch(`${API_URL}/api/jobs`, {
        method: 'POST',
        headers: authHeaders(token, true),
        body: JSON.stringify(newJob),
      });
      if (response.status === 401) {
        setAuthError(true);
        return 'The API token was rejected.';
      }
      if (!response.ok) return readError(response, 'Failed to create job');
      const job = await response.json();
      setJobs((current) => [...current, job]);
      setIsModalOpen(false);
      return null;
    } catch {
      return 'Failed to create job';
    }
  };

  const openModal = () => {
    setModalKey((current) => current + 1);
    setIsModalOpen(true);
  };

  if (isLoading) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow">
        <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <div className="flex h-16 justify-between">
            <div className="flex">
              <div className="flex flex-shrink-0 items-center">
                <h1 className="text-2xl font-bold text-gray-900">OpenSentry</h1>
              </div>
            </div>
            <div className="flex items-center">
              <button
                onClick={openModal}
                className="inline-flex items-center gap-x-2 rounded-md bg-blue-600 px-3.5 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600"
              >
                <PlusIcon className="-ml-0.5 h-5 w-5" aria-hidden="true" />
                New Job
              </button>
            </div>
          </div>
        </div>
      </nav>

      <main className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="px-4 sm:px-0">
          <form onSubmit={saveToken} className="mb-6 rounded-lg bg-white p-4 shadow">
            <label htmlFor="api-token" className="block text-sm font-medium text-gray-700">
              API token
            </label>
            <p className="mt-1 text-sm text-gray-500">
              Management requests use the server <code>API_TOKEN</code>. Ping URLs do not.
              {authError ? ' The current token was rejected.' : ''}
            </p>
            <div className="mt-3 flex flex-col gap-2 sm:flex-row">
              <input
                id="api-token"
                type="password"
                value={tokenDraft}
                onChange={(event) => setTokenDraft(event.target.value)}
                autoComplete="off"
                className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
              />
              <button
                type="submit"
                className="rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white"
              >
                Save token
              </button>
            </div>
          </form>
          {selectedId ? (
            <JobDetail
              jobId={selectedId}
              apiUrl={API_URL}
              token={token}
              apiOrigin={apiOrigin()}
              onBack={() => {
                setSelectedId(null);
                fetchJobs(token);
              }}
              onDelete={handleDelete}
              onAuthError={onAuthError}
            />
          ) : (
            <>
              {token && !authError ? (
                <AlertChannels apiUrl={API_URL} token={token} />
              ) : null}
              <Stats {...stats} />

              <div className="mt-8">
                {loadError ? (
                  <div className="rounded-lg bg-white p-6 text-center shadow">
                    <p className="text-sm text-red-600">{loadError}</p>
                    <button
                      type="button"
                      onClick={() => fetchJobs(token)}
                      className="mt-4 rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white"
                    >
                      Retry
                    </button>
                  </div>
                ) : jobs.length > 0 ? (
                  <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
                    {jobs.map(job => (
                      <JobCard
                        key={job.id}
                        job={job}
                        apiOrigin={apiOrigin()}
                        onOpen={setSelectedId}
                        onDelete={handleDelete}
                      />
                    ))}
                  </div>
                ) : (
                  <div className="text-center">
                    <h3 className="mt-2 text-sm font-semibold text-gray-900">No jobs</h3>
                    <p className="mt-1 text-sm text-gray-500">Get started by creating a new job.</p>
                    <div className="mt-6">
                      <button
                        type="button"
                        onClick={openModal}
                        className="inline-flex items-center gap-x-2 rounded-md bg-blue-600 px-3.5 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600"
                      >
                        <PlusIcon className="-ml-0.5 h-5 w-5" aria-hidden="true" />
                        New Job
                      </button>
                    </div>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </main>

      <NewJobModal
        key={modalKey}
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        onSubmit={handleCreateJob}
      />
    </div>
  );
}

export default App;
