import { useEffect, useState } from 'react';

interface AlertChannelsProps {
  apiUrl: string;
}

export function AlertChannels({ apiUrl }: AlertChannelsProps) {
  const [slack, setSlack] = useState('');
  const [discord, setDiscord] = useState('');
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const response = await fetch(`${apiUrl}/api/channels`, { credentials: 'include' });
        if (!response.ok) throw new Error(await response.text());
        const data = await response.json();
        if (cancelled) return;
        setSlack(data.slack || '');
        setDiscord(data.discord || '');
        setError('');
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Could not load alert channels');
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [apiUrl]);

  async function save(kind: 'slack' | 'discord', webhookURL: string) {
    setStatus('');
    setError('');
    try {
      const response = await fetch(`${apiUrl}/api/channels/${kind}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ webhook_url: webhookURL.trim() }),
      });
      if (!response.ok) {
        throw new Error((await response.text()) || `Could not save ${kind}`);
      }
      setStatus(webhookURL.trim() ? `${kind} webhook saved` : `${kind} webhook cleared`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    }
  }

  return (
    <section className="mb-6 rounded-lg bg-white p-4 shadow">
      <h2 className="text-sm font-medium text-gray-900">Alert channels</h2>
      <p className="mt-1 text-sm text-gray-500">
        Slack and Discord incoming webhooks receive miss and recovery alerts. The URL is a secret.
      </p>
      <ChannelField
        id="slack-webhook"
        label="Slack webhook URL"
        value={slack}
        onChange={setSlack}
        onSave={() => save('slack', slack)}
      />
      <ChannelField
        id="discord-webhook"
        label="Discord webhook URL"
        value={discord}
        onChange={setDiscord}
        onSave={() => save('discord', discord)}
      />
      {status ? <p className="mt-3 text-sm text-green-700">{status}</p> : null}
      {error ? <p className="mt-3 text-sm text-red-600">{error}</p> : null}
    </section>
  );
}

function ChannelField({
  id,
  label,
  value,
  onChange,
  onSave,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  onSave: () => void;
}) {
  return (
    <div className="mt-4">
      <label htmlFor={id} className="block text-sm font-medium text-gray-700">
        {label}
      </label>
      <div className="mt-1 flex flex-col gap-2 sm:flex-row">
        <input
          id={id}
          type="url"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder="https://"
          autoComplete="off"
          className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm"
        />
        <button
          type="button"
          onClick={onSave}
          className="rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white"
        >
          Save
        </button>
      </div>
    </div>
  );
}
