import { useState, useEffect } from 'react';
import { JobCard } from './components/JobCard';
import { Stats } from './components/Stats';
import { NewJobModal } from './components/NewJobModal';
import { PlusIcon } from '@heroicons/react/24/outline';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

interface Job {
  id: string;
  name: string;
  description: string;
  schedule: string;
  status: 'healthy' | 'missing' | 'paused';
  last_ping: string;
  next_expect: string;
}

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    fetchJobs();
  }, []);

  const fetchJobs = async () => {
    try {
      const response = await fetch(`${API_URL}/api/jobs`);
      if (!response.ok) throw new Error('Failed to fetch jobs');
      const data = await response.json();
      setJobs(data);
    } catch (error) {
      console.error('Error fetching jobs:', error);
    } finally {
      setIsLoading(false);
    }
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
      });
      if (!response.ok) throw new Error('Failed to delete job');
      setJobs(jobs.filter(job => job.id !== id));
    } catch (error) {
      console.error('Error deleting job:', error);
    }
  };

  const handleCreateJob = async (newJob: { name: string; description: string; schedule: string; grace_time: number }) => {
    try {
      const response = await fetch(`${API_URL}/api/jobs`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(newJob),
      });
      if (!response.ok) throw new Error('Failed to create job');
      const job = await response.json();
      setJobs([...jobs, job]);
      setIsModalOpen(false);
    } catch (error) {
      console.error('Error creating job:', error);
    }
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
                <h1 className="text-2xl font-bold text-gray-900">CronSentry</h1>
              </div>
            </div>
            <div className="flex items-center">
              <button
                onClick={() => setIsModalOpen(true)}
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
          <Stats {...stats} />
          
          <div className="mt-8">
            {jobs.length > 0 ? (
              <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
                {jobs.map(job => (
                  <JobCard key={job.id} job={job} onDelete={handleDelete} />
                ))}
              </div>
            ) : (
              <div className="text-center">
                <h3 className="mt-2 text-sm font-semibold text-gray-900">No jobs</h3>
                <p className="mt-1 text-sm text-gray-500">Get started by creating a new job.</p>
                <div className="mt-6">
                  <button
                    type="button"
                    onClick={() => setIsModalOpen(true)}
                    className="inline-flex items-center gap-x-2 rounded-md bg-blue-600 px-3.5 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600"
                  >
                    <PlusIcon className="-ml-0.5 h-5 w-5" aria-hidden="true" />
                    New Job
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      </main>

      <NewJobModal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        onSubmit={handleCreateJob}
      />
    </div>
  );
}

export default App;
