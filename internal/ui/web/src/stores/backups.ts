import { apiFetch } from '$lib/api';

export interface BackupArchive {
  name: string;
  size: number;
  taken: string;
}

export interface SiteBackups {
  schedule?: string;
  verify?: string;
  disabled: boolean;
  keep: { daily: number; weekly: number; monthly: number };
  archives: BackupArchive[];
  key_path: string;
}

export interface BackupActionResult {
  ok: boolean;
  error?: string;
  archive?: string;
  files?: number;
  tables?: number;
  pruned?: number;
  note?: string;
}

const base = (domain: string) => `/api/sites/${encodeURIComponent(domain)}/backups`;

export async function loadSiteBackups(domain: string): Promise<SiteBackups | null> {
  try {
    const res = await apiFetch(base(domain));
    if (!res.ok) return null;
    return (await res.json()) as SiteBackups;
  } catch {
    return null;
  }
}

async function post(domain: string, body: Record<string, unknown>): Promise<BackupActionResult> {
  try {
    const res = await apiFetch(base(domain), {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body)
    });
    return (await res.json()) as BackupActionResult;
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

export const runBackup = (domain: string) => post(domain, { action: 'run' });
export const verifyBackup = (domain: string) => post(domain, { action: 'verify' });
export const unscheduleBackup = (domain: string) => post(domain, { action: 'unschedule' });
export const scheduleBackup = (
  domain: string,
  schedule: string,
  verify: string,
  keep: { daily: number; weekly: number; monthly: number }
) => post(domain, { action: 'schedule', schedule, verify, keep });

// The server's own state, which is not a site's and so has no site to hang
// off. Everything servlo knows that is not files or data: the registry, the
// connections, the accounts, the settings. It is what makes a rebuild onto a
// fresh machine possible.

export interface StateArchive {
  name: string;
  size: number;
  taken: string;
}

export interface ServerStateResult {
  archives?: StateArchive[];
  directory?: string;
  error?: string;
}

export interface StateTaken {
  ok?: boolean;
  error?: string;
  name?: string;
  size?: number;
  files?: number;
  // One per destination the archive could not be copied to. The archive is on
  // the server either way, so these come back beside a successful write rather
  // than instead of one, and the card has to say so: an archive that stayed on
  // the machine it is a backup of is the one case this backup exists for.
  send_errors?: string[];
}

export async function loadServerState(): Promise<ServerStateResult> {
  const res = await apiFetch('/api/backup/state');
  if (!res.ok) throw new Error(await res.text());
  return (await res.json()) as ServerStateResult;
}

export async function backUpServerState(): Promise<StateTaken> {
  const res = await apiFetch('/api/backup/state', { method: 'POST' });
  return (await res.json()) as StateTaken;
}
