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

export interface CloneTestResult {
  ok?: boolean;
  reason?: string;
  greeting?: string;
}

export async function deployKeyFor(domain: string): Promise<{ public?: string; error?: string }> {
  const res = await apiFetch('/api/sites/deploy-key', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ domain })
  });
  return (await res.json()) as { public?: string; error?: string };
}

export async function testClone(body: { domain: string; repository: string }): Promise<CloneTestResult> {
  const res = await apiFetch('/api/sites/clone-test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
  return (await res.json()) as CloneTestResult;
}

export async function cloneSite(body: {
  domain: string;
  path: string;
  repository: string;
  php_version?: string;
  public_dir?: string;
}): Promise<AddSiteResult> {
  const res = await apiFetch('/api/sites/clone', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
  return (await res.json()) as AddSiteResult;
}

// The archive goes as multipart rather than JSON, so apiFetch must not set a
// Content-Type: the browser has to write the boundary itself.
export async function uploadSite(body: {
  domain: string;
  path: string;
  archive: File;
  php_version?: string;
  public_dir?: string;
}): Promise<AddSiteResult> {
  const form = new FormData();
  form.append('domain', body.domain);
  form.append('path', body.path);
  if (body.php_version) form.append('php_version', body.php_version);
  if (body.public_dir) form.append('public_dir', body.public_dir);
  form.append('archive', body.archive);

  const res = await apiFetch('/api/sites/upload', { method: 'POST', body: form });
  return (await res.json()) as AddSiteResult;
}

// Installing an application is the fourth source. It differs from the other
// three in one way that shapes the form: it comes back with credentials that
// exist nowhere else, so the modal has to stop and show them rather than close
// onto the new site.

export interface AppOption {
  name: string;
  label: string;
  description: string;
  version: string;
  needs_database: boolean;
  // True for an application whose own installer servlo cannot drive, so the
  // form can say where the install stops before anybody starts it.
  self_setup: boolean;
}

export interface AppInstallResult {
  ok?: boolean;
  error?: string;
  site?: string;
  domain?: string;
  path?: string;
  admin_user?: string;
  admin_password?: string;
  database?: string;
  note?: string;
}

export async function loadApps(): Promise<AppOption[]> {
  const res = await apiFetch('/api/apps');
  if (!res.ok) throw new Error(await res.text());
  const body = (await res.json()) as { apps?: AppOption[] };
  return body.apps ?? [];
}

export async function installApp(body: {
  app: string;
  domain: string;
  path: string;
  admin_user?: string;
  admin_email?: string;
  site_title?: string;
}): Promise<AppInstallResult> {
  const res = await apiFetch('/api/sites/app', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });
  return (await res.json()) as AppInstallResult;
}
