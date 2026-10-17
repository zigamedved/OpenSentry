import { useCallback, useEffect, useState } from 'react';

interface Workspace {
  id: string;
  name: string;
  role: 'admin' | 'member';
  active: boolean;
}

interface Member {
  user_id: string;
  email: string;
  name: string;
  role: 'admin' | 'member';
}

interface Invite {
  id: string;
  workspace_id: string;
  workspace_name: string;
  email: string;
  role: string;
  token: string;
}

function localInputValue(date: Date) {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

async function readError(response: Response, fallback: string) {
  const text = (await response.text()).trim();
  return text || fallback;
}

export function WorkspacePanel({
  apiUrl,
  onChanged,
  onAuthError,
}: {
  apiUrl: string;
  onChanged: () => void;
  onAuthError: () => void;
}) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [mine, setMine] = useState<Invite[]>([]);
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<'member' | 'admin'>('member');
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [exportFrom, setExportFrom] = useState(() => localInputValue(new Date(Date.now() - 24 * 60 * 60 * 1000)));
  const [exportTo, setExportTo] = useState(() => localInputValue(new Date(Date.now() + 60 * 60 * 1000)));
  const [exportText, setExportText] = useState('');
  const [busy, setBusy] = useState(false);

  const active = workspaces.find((workspace) => workspace.active) ?? workspaces[0];

  const load = useCallback(async () => {
    const [wsRes, inviteRes] = await Promise.all([
      fetch(`${apiUrl}/api/workspaces`, { credentials: 'include' }),
      fetch(`${apiUrl}/api/invites`, { credentials: 'include' }),
    ]);
    if (wsRes.status === 401 || inviteRes.status === 401) {
      onAuthError();
      return;
    }
    if (!wsRes.ok) {
      setError(await readError(wsRes, 'Could not load workspaces'));
      return;
    }
    const list = (await wsRes.json()) as Workspace[];
    setWorkspaces(list);
    const current = list.find((workspace) => workspace.active) ?? list[0];
    if (!current) return;
    const memberRes = await fetch(`${apiUrl}/api/workspaces/${current.id}/members`, { credentials: 'include' });
    if (memberRes.status === 401) {
      onAuthError();
      return;
    }
    if (memberRes.ok) setMembers((await memberRes.json()) as Member[]);
    if (current.role === 'admin') {
      const pending = await fetch(`${apiUrl}/api/workspaces/${current.id}/invites`, { credentials: 'include' });
      if (pending.ok) setInvites((await pending.json()) as Invite[]);
    } else {
      setInvites([]);
    }
    if (inviteRes.ok) setMine((await inviteRes.json()) as Invite[]);
  }, [apiUrl, onAuthError]);

  useEffect(() => {
    void load().catch(() => setError('Could not load workspaces'));
  }, [load]);

  const switchTo = async (id: string) => {
    setError(null);
    setExportText('');
    const response = await fetch(`${apiUrl}/api/workspaces/${id}/switch`, { method: 'POST', credentials: 'include' });
    if (response.status === 401) {
      onAuthError();
      return;
    }
    if (!response.ok) {
      setError(await readError(response, 'Could not switch workspace'));
      return;
    }
    await load();
    onChanged();
  };

  const invite = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!active) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`${apiUrl}/api/workspaces/${active.id}/invites`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, role }),
      });
      if (response.status === 401) {
        onAuthError();
        return;
      }
      if (!response.ok) {
        setError(await readError(response, 'Could not invite'));
        return;
      }
      setNotice(`${email} can join from the sign-in page. A new account joins this workspace automatically.`);
      setEmail('');
      await load();
    } finally {
      setBusy(false);
    }
  };

  const accept = async (token: string) => {
    setError(null);
    const response = await fetch(`${apiUrl}/api/workspaces/invites/${token}/accept`, {
      method: 'POST',
      credentials: 'include',
    });
    if (response.status === 401) {
      onAuthError();
      return;
    }
    if (!response.ok) {
      setError(await readError(response, 'Could not accept invite'));
      return;
    }
    setExportText('');
    await load();
    onChanged();
  };

  const removeMember = async (userID: string) => {
    if (!active) return;
    setError(null);
    const response = await fetch(`${apiUrl}/api/workspaces/${active.id}/members/${userID}`, {
      method: 'DELETE',
      credentials: 'include',
    });
    if (!response.ok) {
      setError(await readError(response, 'Could not remove member'));
      return;
    }
    await load();
  };

  const changeRole = async (userID: string, next: string) => {
    if (!active) return;
    setError(null);
    const response = await fetch(`${apiUrl}/api/workspaces/${active.id}/members/${userID}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: next }),
    });
    if (!response.ok) {
      setError(await readError(response, 'Could not change role'));
      await load();
      return;
    }
    await load();
  };

  const showExport = async () => {
    if (!active) return;
    setError(null);
    setBusy(true);
    try {
      const from = new Date(exportFrom).toISOString();
      const to = new Date(exportTo).toISOString();
      const response = await fetch(
        `${apiUrl}/api/workspaces/${active.id}/export?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&format=json`,
        { credentials: 'include' },
      );
      const text = await response.text();
      if (!response.ok) {
        setExportText('');
        setError(text.trim() || 'Could not export');
        return;
      }
      setExportText(JSON.stringify(JSON.parse(text), null, 2));
    } catch {
      setError('Could not export');
    } finally {
      setBusy(false);
    }
  };

  if (!active) return null;

  return (
    <div className="mb-6 rounded-lg bg-white p-4 shadow">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-gray-900">Workspace</h2>
          <p className="mt-1 text-sm text-gray-500">Jobs in the list belong to the workspace selected here.</p>
        </div>
        <label className="text-sm text-gray-700">
          Active
          <select
            id="workspace-select"
            className="ml-2 rounded-md border border-gray-300 px-2 py-1.5 text-sm"
            value={active.id}
            onChange={(event) => void switchTo(event.target.value)}
          >
            {workspaces.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name} ({workspace.role})
              </option>
            ))}
          </select>
        </label>
      </div>

      {mine.length > 0 ? (
        <div id="pending-invites" className="mt-4 rounded-md border border-blue-200 bg-blue-50 p-3">
          <p className="text-sm font-medium text-blue-900">You have a workspace invite</p>
          <ul className="mt-2 space-y-2">
            {mine.map((invite) => (
              <li key={invite.id} className="flex flex-wrap items-center justify-between gap-2 text-sm text-blue-900">
                <span>{invite.workspace_name} · {invite.role}</span>
                <button
                  type="button"
                  className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-blue-500"
                  onClick={() => void accept(invite.token)}
                >
                  Accept
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {error ? <p className="mt-3 text-sm text-red-600">{error}</p> : null}
      {notice ? <p className="mt-3 text-sm text-green-700">{notice}</p> : null}

      <div className="mt-4 grid gap-6 lg:grid-cols-2">
        <div>
          <h3 className="text-sm font-medium text-gray-900">Team</h3>
          <ul className="mt-2 divide-y divide-gray-100">
            {members.map((member) => (
              <li key={member.user_id} className="flex items-center justify-between gap-2 py-2 text-sm">
                <span className="text-gray-700">{member.email}</span>
                {active.role === 'admin' ? (
                  <span className="flex items-center gap-2">
                    <select
                      aria-label={`Role for ${member.email}`}
                      className="rounded-md border border-gray-300 px-2 py-1 text-sm"
                      value={member.role}
                      onChange={(event) => void changeRole(member.user_id, event.target.value)}
                    >
                      <option value="admin">admin</option>
                      <option value="member">member</option>
                    </select>
                    <button
                      type="button"
                      className="text-sm font-medium text-red-600 hover:text-red-800"
                      onClick={() => void removeMember(member.user_id)}
                    >
                      Remove
                    </button>
                  </span>
                ) : (
                  <span className="text-gray-500">{member.role}</span>
                )}
              </li>
            ))}
          </ul>
          {active.role === 'admin' ? (
            <form onSubmit={invite} className="mt-3 flex flex-wrap items-end gap-2">
              <label className="text-sm text-gray-700">
                Invite email
                <input
                  id="invite-email"
                  type="email"
                  required
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  className="mt-1 block rounded-md border border-gray-300 px-2 py-1.5 text-sm"
                />
              </label>
              <label className="text-sm text-gray-700">
                Role
                <select
                  id="invite-role"
                  value={role}
                  onChange={(event) => setRole(event.target.value as 'member' | 'admin')}
                  className="mt-1 block rounded-md border border-gray-300 px-2 py-1.5 text-sm"
                >
                  <option value="member">member</option>
                  <option value="admin">admin</option>
                </select>
              </label>
              <button
                id="invite-submit"
                type="submit"
                disabled={busy}
                className="rounded-md bg-gray-900 px-3 py-2 text-sm font-semibold text-white hover:bg-gray-700 disabled:opacity-50"
              >
                Invite
              </button>
            </form>
          ) : null}
          {invites.length > 0 ? (
            <p className="mt-2 text-sm text-gray-500">
              Waiting: {invites.map((invite) => invite.email).join(', ')}
            </p>
          ) : null}
        </div>

        <div>
          <h3 className="text-sm font-medium text-gray-900">Audit export</h3>
          <p className="mt-1 text-sm text-gray-500">Events and notifications in this range. The end time is not included.</p>
          <div className="mt-2 flex flex-wrap items-end gap-2">
            <label className="text-sm text-gray-700">
              From
              <input
                id="export-from"
                type="datetime-local"
                value={exportFrom}
                onChange={(event) => setExportFrom(event.target.value)}
                className="mt-1 block rounded-md border border-gray-300 px-2 py-1.5 text-sm"
              />
            </label>
            <label className="text-sm text-gray-700">
              To
              <input
                id="export-to"
                type="datetime-local"
                value={exportTo}
                onChange={(event) => setExportTo(event.target.value)}
                className="mt-1 block rounded-md border border-gray-300 px-2 py-1.5 text-sm"
              />
            </label>
            <button
              id="export-show"
              type="button"
              disabled={busy}
              onClick={() => void showExport()}
              className="rounded-md border border-gray-300 px-3 py-2 text-sm font-semibold text-gray-800 hover:bg-gray-50 disabled:opacity-50"
            >
              Show export
            </button>
          </div>
          {exportText ? (
            <pre id="export-result" className="mt-3 max-h-64 overflow-auto rounded-md bg-gray-900 p-3 text-xs text-gray-100">
              {exportText}
            </pre>
          ) : null}
        </div>
      </div>
    </div>
  );
}
