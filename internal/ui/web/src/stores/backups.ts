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
