import { apiFetch } from '$lib/api';

// Adding a site is two requests, not one. The form asks about a path first so
// it can show what servlo found there before anything is registered, and only
// the second request changes the machine.

export interface DirectoryReport {
  path: string;
  exists: boolean;
  has_content: boolean;
  framework: string;
  public_dir: string;
  php_version: string;
  error?: string;
}

export interface AddSiteResult {
  ok?: boolean;
  error?: string;
  domain?: string;
  name?: string;
  path?: string;
  framework?: string;
  php_version?: string;
  public_dir?: string;
  created?: boolean;
  warning?: string;
}

export async function inspectDirectory(path: string): Promise<DirectoryReport> {
  const res = await apiFetch('/api/sites/inspect?path=' + encodeURIComponent(path));
  if (!res.ok) throw new Error(await res.text());
  return (await res.json()) as DirectoryReport;
}

export async function addSite(body: {
  domain: string;
  path: string;
  php_version?: string;
  public_dir?: string;
}): Promise<AddSiteResult> {
  const res = await apiFetch('/api/sites/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
  return (await res.json()) as AddSiteResult;
}
