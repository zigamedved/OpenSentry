import { useEffect, useState } from 'react';

interface Billing {
  mode: 'self_host' | 'hosted';
  plan: string;
  used_checks: number;
  max_checks: number | null;
  portal: boolean;
  message: string;
}

export function BillingBanner({ apiUrl, jobCount }: { apiUrl: string; jobCount: number }) {
  const [billing, setBilling] = useState<Billing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const response = await fetch(`${apiUrl}/api/billing`, { credentials: 'include' });
        if (!response.ok) {
          if (!cancelled) setError('Could not load plan');
          return;
        }
        const body = (await response.json()) as Billing;
        if (!cancelled) {
          setBilling(body);
          setError(null);
        }
      } catch {
        if (!cancelled) setError('Could not load plan');
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [apiUrl, jobCount]);

  const open = async (path: string) => {
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(`${apiUrl}${path}`, { method: 'POST', credentials: 'include' });
      if (!response.ok) {
        const text = (await response.text()).trim();
        setError(text || 'Billing request failed');
        return;
      }
      const body = (await response.json()) as { url?: string };
      if (!body.url) {
        setError('Billing request failed');
        return;
      }
      window.location.href = body.url;
    } catch {
      setError('Billing request failed');
    } finally {
      setBusy(false);
    }
  };

  if (!billing && !error) return null;

  return (
    <div className="mb-6 rounded-lg bg-white p-4 shadow">
      <p className="text-sm text-gray-700">{billing?.message}</p>
      {error ? <p className="mt-2 text-sm text-red-600">{error}</p> : null}
      {billing?.mode === 'hosted' ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {billing.plan !== 'pro' ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => open('/api/billing/checkout')}
              className="rounded-md bg-blue-600 px-3 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
            >
              Upgrade
            </button>
          ) : null}
          {billing.portal ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => open('/api/billing/portal')}
              className="rounded-md border border-gray-300 px-3 py-2 text-sm font-semibold text-gray-800 hover:bg-gray-50 disabled:opacity-50"
            >
              Manage billing
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
