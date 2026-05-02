import { apiJson } from '$lib/api';

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

function branchQuery(branch?: string): string {
  return branch ? `?branch=${encodeURIComponent(branch)}` : '';
}

export async function listAppLogFiles(domain: string, branch?: string): Promise<AppLogFile[]> {
  try {
    const res = await apiJson<{ files?: AppLogFile[] }>(
      `/api/app-logs/${encodeURIComponent(domain)}${branchQuery(branch)}`
    );
    return Array.isArray(res.files) ? res.files : [];
  } catch {
    return [];
  }
}

export async function loadAppLogEntries(
  domain: string,
  file: string,
  showAll: boolean,
  branch?: string
): Promise<AppLogEntry[]> {
  try {
    const limit = showAll ? 0 : 100;
    const params = new URLSearchParams({ limit: String(limit) });
    if (branch) params.set('branch', branch);
    const res = await apiJson<{ entries?: AppLogEntry[] }>(
      `/api/app-logs/${encodeURIComponent(domain)}/${encodeURIComponent(file)}?${params.toString()}`
    );
    return Array.isArray(res.entries) ? res.entries : [];
  } catch {
    return [];
  }
}
