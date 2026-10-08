import { useEffect, useState } from 'react';

interface NewJobModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (job: { name: string; description: string; schedule: string; grace_time: number }) => Promise<string | null>;
}

export function NewJobModal({ isOpen, onClose, onSubmit }: NewJobModalProps) {
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (isOpen) {
      setError(null);
      setSaving(false);
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    setSaving(true);
    setError(null);
    const message = await onSubmit({
      name: formData.get('name') as string,
      description: formData.get('description') as string,
      schedule: formData.get('schedule') as string,
      grace_time: Number(formData.get('grace_time')),
    });
    setSaving(false);
    if (message) setError(message);
  };

  return (
    <div className="fixed inset-0 bg-gray-500 bg-opacity-75 flex items-center justify-center p-4">
      <div className="bg-white rounded-lg max-w-md w-full p-6">
        <h2 className="text-xl font-semibold text-gray-900 mb-4">Add New Job</h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="name" className="block text-sm font-medium text-gray-700">
              Job Name
            </label>
            <input
              type="text"
              name="name"
              id="name"
              required
              className="mt-1 block w-full rounded-md border border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            />
          </div>

          <div>
            <label htmlFor="description" className="block text-sm font-medium text-gray-700">
              Description
            </label>
            <textarea
              name="description"
              id="description"
              rows={3}
              className="mt-1 block w-full rounded-md border border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            />
          </div>

          <div>
            <label htmlFor="schedule" className="block text-sm font-medium text-gray-700">
              Cron Schedule
            </label>
            <input
              type="text"
              name="schedule"
              id="schedule"
              required
              placeholder="* * * * *"
              className="mt-1 block w-full rounded-md border border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            />
          </div>

          <div>
            <label htmlFor="grace_time" className="block text-sm font-medium text-gray-700">
              Grace Time (minutes)
            </label>
            <input
              type="number"
              name="grace_time"
              id="grace_time"
              required
              min="1"
              defaultValue="5"
              className="mt-1 block w-full rounded-md border border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
            />
          </div>

          {error ? <p className="text-sm text-red-600">{error}</p> : null}

          <div className="mt-6 flex justify-end space-x-3">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving}
              className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 border border-transparent rounded-md hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 disabled:opacity-50"
            >
              Create Job
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
