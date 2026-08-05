import { m } from '../paraglide/messages.js';
import { apiJson, apiFetch } from '$lib/api';

export interface AppLogFile {
  name: string;
  size?: number;
  modified?: string;
}

export interface AppLogEntry {
  level?: string;
  date?: string;
  message?: string;
  detail?: string;
}

export async function listAppLogFiles(domain: string): Promise<AppLogFile[]> {
  try {
    const res = await apiJson<{ files?: AppLogFile[] }>(
      `/api/app-logs/${encodeURIComponent(domain)}`
    );
    return Array.isArray(res.files) ? res.files : [];
  } catch {
    return [];
  }
}

export interface ClearAppLogsResult {
  ok: boolean;
  filesCleared: number;
  bytesCleared: number;
  error?: string;
}

// clearAppLogs deletes the project's log files to reclaim disk. The active log
// is recreated by the app on its next write.
export async function clearAppLogs(domain: string): Promise<ClearAppLogsResult> {
  try {
    const res = await apiFetch(
      `/api/app-logs/${encodeURIComponent(domain)}/clear`,
      { method: 'POST' }
    );
    const data = (await res.json()) as {
      ok?: boolean;
      files_cleared?: number;
      bytes_cleared?: number;
      error?: string;
    };
    return {
      ok: Boolean(data.ok),
      filesCleared: data.files_cleared ?? 0,
      bytesCleared: data.bytes_cleared ?? 0,
      error: data.error
    };
  } catch (e) {
    return { ok: false, filesCleared: 0, bytesCleared: 0, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

export async function loadAppLogEntries(
  domain: string,
  file: string,
  showAll: boolean
): Promise<AppLogEntry[]> {
  try {
    const limit = showAll ? 0 : 100;
    const params = new URLSearchParams({ limit: String(limit) });
    const res = await apiJson<{ entries?: AppLogEntry[] }>(
      `/api/app-logs/${encodeURIComponent(domain)}/${encodeURIComponent(file)}?${params.toString()}`
    );
    return Array.isArray(res.entries) ? res.entries : [];
  } catch {
    return [];
  }
}
