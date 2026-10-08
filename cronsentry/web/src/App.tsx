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

interface Account {
  id: string;
  email: string;
  name: string;
}

function apiOrigin() {
  if (API_URL) return API_URL.replace(/\/$/, '');
  return window.location.origin;
}

async function readError(response: Response, fallback: string) {
  const text = (await response.text()).trim();
  return text || fallback;
}

// The session cookie is HttpOnly, so a rejected session has to be cleared by
// the API. Logout is public and always expires the cookie.
async function forgetCookie() {
  try {
    await fetch(`${API_URL}/api/logout`, { method: 'POST', credentials: 'include' });
  } catch {
    // The sign-in form is still the right place if the network drops.
  }
}

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [modalKey, setModalKey] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [account, setAccount] = useState<Account | null>(null);
  const [mode, setMode] = useState<'login' | 'register'>('register');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const fetchJobs = useCallback(async () => {
    try {
      const response = await fetch(`${API_URL}/api/jobs`, { credentials: 'include' });
      if (response.status === 401) {
        await forgetCookie();
        setAccount(null);
        setFormError('Sign in again.');
        setLoadError(null);
        setJobs([]);
        return;
      }
      if (!response.ok) {
        setLoadError(await readError(response, 'Failed to load jobs'));
        return;
      }
      const data = await response.json();
      setFormError(null);
      setLoadError(null);
      setJobs(data);
    } catch {
      setLoadError('Failed to load jobs');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function loadSession() {
      try {
        const response = await fetch(`${API_URL}/api/me`, { credentials: 'include' });
        if (cancelled) return;
        if (!response.ok) {
          if (response.status === 401) await forgetCookie();
          setAccount(null);
          setIsLoading(false);
          return;
        }
        const me = (await response.json()) as Account;
        if (cancelled) return;
        setAccount(me);
        await fetchJobs();
      } catch {
        if (!cancelled) {
          setAccount(null);
          setIsLoading(false);
        }
      }
    }
    loadSession();
    return () => {
      cancelled = true;
    };
  }, [fetchJobs]);

  const onAuthError = useCallback(() => {
    void forgetCookie();
    setAccount(null);
    setFormError('Sign in again.');
    setJobs([]);
    setSelectedId(null);
  }, []);

  const submitAccount = async (event: React.FormEvent) => {
    event.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const path = mode === 'register' ? '/api/register' : '/api/login';
      const payload = mode === 'register'
        ? { email, password, name }
        : { email, password };
      const response = await fetch(`${API_URL}${path}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!response.ok) {
        setFormError(await readError(response, mode === 'register' ? 'Could not create account' : 'Could not sign in'));
        return;
      }
      const data = await response.json();
      setAccount(data.user);
      setPassword('');
      setIsLoading(true);
      await fetchJobs();
    } catch {
      setFormError(mode === 'register' ? 'Could not create account' : 'Could not sign in');
    } finally {
      setSubmitting(false);
    }
  };

  const logout = async () => {
    await forgetCookie();
    setAccount(null);
    setJobs([]);
    setSelectedId(null);
    setFormError(null);
    setMode('login');
  };

  const stats = {
    total: jobs.length,
    healthy: jobs.filter(job => job.status === 'healthy').length,
    missing: jobs.filter(job => job.status === 'missing').length,
  };

  const handleDelete = async (id: string) => {
    try {
      const response = await fetch(`${API_URL}/api/jobs/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      });
      if (response.status === 401) {
        onAuthError();
        return 'Sign in again.';
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
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newJob),
      });
      if (response.status === 401) {
        onAuthError();
        return 'Sign in again.';
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
            <div className="flex items-center gap-3">
              {account ? (
                <>
                  <span className="hidden text-sm text-gray-600 sm:inline">{account.email}</span>
                  <button
                    type="button"
                    onClick={logout}
                    className="rounded-md border border-gray-300 px-3 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-50"
                  >
                    Log out
                  </button>
                  <button
                    onClick={openModal}
                    className="inline-flex items-center gap-x-2 rounded-md bg-blue-600 px-3.5 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600"
                  >
                    <PlusIcon className="-ml-0.5 h-5 w-5" aria-hidden="true" />
                    New Job
                  </button>
                </>
              ) : null}
            </div>
          </div>
        </div>
      </nav>

      <main className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="px-4 sm:px-0">
          {!account ? (
            <form onSubmit={submitAccount} className="mx-auto max-w-md rounded-lg bg-white p-6 shadow">
              <h2 className="text-lg font-semibold text-gray-900">
                {mode === 'register' ? 'Create an account' : 'Sign in'}
              </h2>
              <p className="mt-1 text-sm text-gray-500">
                Each account sees only its own jobs. Ping URLs stay public.
              </p>
              {formError ? <p className="mt-3 text-sm text-red-600">{formError}</p> : null}
              {mode === 'register' ? (
                <div className="mt-4">
                  <label htmlFor="name" className="block text-sm font-medium text-gray-700">Name</label>
                  <input
                    id="name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    required
                    autoComplete="name"
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
                  />
                </div>
              ) : null}
              <div className="mt-4">
                <label htmlFor="email" className="block text-sm font-medium text-gray-700">Email</label>
                <input
                  id="email"
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  required
                  autoComplete="email"
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
                />
              </div>
              <div className="mt-4">
                <label htmlFor="password" className="block text-sm font-medium text-gray-700">Password</label>
                <input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                  minLength={mode === 'register' ? 8 : undefined}
                  autoComplete={mode === 'register' ? 'new-password' : 'current-password'}
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
                />
              </div>
              <button
                type="submit"
                disabled={submitting}
                className="mt-6 w-full rounded-md bg-blue-600 px-3 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
              >
                {mode === 'register' ? 'Create account' : 'Sign in'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setMode(mode === 'register' ? 'login' : 'register');
                  setFormError(null);
                }}
                className="mt-3 w-full text-sm font-medium text-blue-600 hover:text-blue-800"
              >
                {mode === 'register' ? 'Already have an account? Sign in' : 'Need an account? Create one'}
              </button>
            </form>
          ) : selectedId ? (
            <JobDetail
              jobId={selectedId}
              apiUrl={API_URL}
              apiOrigin={apiOrigin()}
              onBack={() => {
                setSelectedId(null);
                fetchJobs();
              }}
              onDelete={handleDelete}
              onAuthError={onAuthError}
            />
          ) : (
            <>
              <p className="mb-4 text-sm text-gray-600 sm:hidden">{account.email}</p>
              <AlertChannels apiUrl={API_URL} />
              <Stats {...stats} />

              <div className="mt-8">
                {loadError ? (
                  <div className="rounded-lg bg-white p-6 text-center shadow">
                    <p className="text-sm text-red-600">{loadError}</p>
                    <button
                      type="button"
                      onClick={() => fetchJobs()}
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
