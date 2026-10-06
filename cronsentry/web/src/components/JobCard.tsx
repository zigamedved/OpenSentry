import { useState } from 'react';

interface Job {
  id: string;
  name: string;
  description: string;
  schedule: string;
  status: 'healthy' | 'late' | 'missing' | 'paused';
  last_ping: string;
  next_expect: string;
}

interface JobCardProps {
  job: Job;
  apiOrigin: string;
  onDelete: (id: string) => void;
}

export function pingCurl(apiOrigin: string, jobId: string) {
  const root = apiOrigin.replace(/\/$/, '');
  return `curl -X POST ${root}/api/ping/${jobId}`;
}

export function JobCard({ job, apiOrigin, onDelete }: JobCardProps) {
  const [copied, setCopied] = useState(false);
  const curl = pingCurl(apiOrigin, job.id);
  const statusColors = {
    healthy: 'bg-green-100 text-green-800',
    late: 'bg-yellow-100 text-yellow-800',
    missing: 'bg-red-100 text-red-800',
    paused: 'bg-gray-100 text-gray-800'
  };

  return (
    <div className="bg-white rounded-lg shadow p-6 hover:shadow-md transition-shadow">
      <div className="flex justify-between items-start">
        <div>
          <h3 className="text-lg font-semibold text-gray-900">{job.name}</h3>
          <p className="mt-1 text-sm text-gray-500">{job.description}</p>
        </div>
        <span className={`px-2.5 py-0.5 rounded-full text-xs font-medium ${statusColors[job.status]}`}>
          {job.status}
        </span>
      </div>
      
      <div className="mt-4 space-y-2">
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Schedule:</span>
          <span className="font-medium">{job.schedule}</span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Last Ping:</span>
          <span className="font-medium">{new Date(job.last_ping).toLocaleString()}</span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-gray-500">Next Expected:</span>
          <span className="font-medium">{new Date(job.next_expect).toLocaleString()}</span>
        </div>
      </div>

      <div className="mt-4">
        <p className="text-xs font-medium text-gray-500">Ping command</p>
        <p className="mt-1 text-xs text-gray-500">The job id in this URL is a secret. Anyone who has it can record a ping.</p>
        <div className="mt-2 flex items-center gap-2">
          <code className="block flex-1 overflow-x-auto rounded bg-gray-50 px-2 py-1 text-xs text-gray-800">{curl}</code>
          <button
            type="button"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(curl);
              } catch {
                const area = document.createElement('textarea');
                area.value = curl;
                document.body.appendChild(area);
                area.select();
                document.execCommand('copy');
                area.remove();
              }
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            }}
            className="shrink-0 rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-200"
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      </div>

      <div className="mt-4 flex justify-end space-x-2">
        <button
          onClick={() => onDelete(job.id)}
          className="px-3 py-1 text-sm text-red-600 hover:text-red-800"
        >
          Delete
        </button>
      </div>
    </div>
  );
} 