import { CheckCircleIcon, XCircleIcon, ClipboardDocumentListIcon } from '@heroicons/react/24/outline';

interface StatsProps {
  total: number;
  healthy: number;
  missing: number;
}

export function Stats({ total, healthy, missing }: StatsProps) {
  return (
    <dl className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
      <div className="relative overflow-hidden rounded-lg bg-white px-4 pb-5 pt-5 shadow sm:px-6 sm:pt-6">
        <dt>
          <div className="absolute rounded-md bg-green-500 p-3">
            <CheckCircleIcon className="h-5 w-5 text-white" aria-hidden="true" />
          </div>
          <p className="ml-16 truncate text-sm font-medium text-gray-500">Healthy Jobs</p>
        </dt>
        <dd className="ml-16 flex items-baseline">
          <p className="text-2xl font-semibold text-gray-900">{healthy}</p>
        </dd>
      </div>

      <div className="relative overflow-hidden rounded-lg bg-white px-4 pb-5 pt-5 shadow sm:px-6 sm:pt-6">
        <dt>
          <div className="absolute rounded-md bg-red-500 p-3">
            <XCircleIcon className="h-5 w-5 text-white" aria-hidden="true" />
          </div>
          <p className="ml-16 truncate text-sm font-medium text-gray-500">Missing Jobs</p>
        </dt>
        <dd className="ml-16 flex items-baseline">
          <p className="text-2xl font-semibold text-gray-900">{missing}</p>
        </dd>
      </div>

      <div className="relative overflow-hidden rounded-lg bg-white px-4 pb-5 pt-5 shadow sm:px-6 sm:pt-6">
        <dt>
          <div className="absolute rounded-md bg-blue-500 p-3">
            <ClipboardDocumentListIcon className="h-5 w-5 text-white" aria-hidden="true" />
          </div>
          <p className="ml-16 truncate text-sm font-medium text-gray-500">Total Jobs</p>
        </dt>
        <dd className="ml-16 flex items-baseline">
          <p className="text-2xl font-semibold text-gray-900">{total}</p>
        </dd>
      </div>
    </dl>
  );
} 