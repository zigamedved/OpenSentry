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
  onDelete: (id: string) => void;
}

export function JobCard({ job, onDelete }: JobCardProps) {
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