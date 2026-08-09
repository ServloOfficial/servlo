import { apiFetch, apiUrl, decodeJSONResult, getCSRFToken } from '$lib/api';

// The file manager's client. One module, because every call is the same shape:
// a site, a path inside it, and a server that has already decided whether that
// path is allowed. Nothing here validates a path; the panel is not where that
// decision is safe to make.

export interface FileEntry {
  name: string;
  path: string;
  dir: boolean;
  size: number;
  mode: string;
  modified: number;
  symlink: boolean;
  escapes?: boolean;
}

export interface FileListing {
  path: string;
  entries: FileEntry[];
  truncated: boolean;
  root: string;
  error?: string;
}

export interface FileContent {
  path: string;
  text: string;
  size: number;
  mode: string;
  binary: boolean;
  truncated: boolean;
  error?: string;
}

export interface FileResult {
  ok?: boolean;
  error?: string;
  path?: string;
}

export interface UnzipResult extends FileResult {
  files?: number;
  bytes?: number;
}

export interface PermissionRule {
  name: string;
  octal: string;
  reason: string;
  count: number;
  changes: number;
  samples?: string[];
}

export interface PermissionPlan {
  rules: PermissionRule[];
  changes: number;
  skipped: number;
  truncated: boolean;
  applied: number;
  failed?: string[];
  ok?: boolean;
  error?: string;
}

const base = (domain: string) => `/api/sites/${encodeURIComponent(domain)}/files`;

export async function loadFiles(domain: string, path: string): Promise<FileListing> {
  const res = await apiFetch(`${base(domain)}?path=${encodeURIComponent(path)}`);
  return (await res.json()) as FileListing;
}

export async function loadFileContent(domain: string, path: string): Promise<FileContent> {
  const res = await apiFetch(`${base(domain)}/content?path=${encodeURIComponent(path)}`);
  return (await res.json()) as FileContent;
}

export async function saveFileContent(
  domain: string,
  path: string,
  content: string
): Promise<FileResult> {
  const res = await apiFetch(`${base(domain)}/content`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content })
  });
  return decodeJSONResult<FileResult>(res);
}

export async function deleteFileEntry(domain: string, path: string): Promise<FileResult> {
  const res = await apiFetch(`${base(domain)}/entry?path=${encodeURIComponent(path)}`, {
    method: 'DELETE'
  });
  return decodeJSONResult<FileResult>(res);
}

export async function unzipFile(domain: string, path: string): Promise<UnzipResult> {
  const res = await apiFetch(`${base(domain)}/unzip`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path })
  });
  return decodeJSONResult<UnzipResult>(res);
}

export async function loadPermissionPlan(domain: string): Promise<PermissionPlan> {
  const res = await apiFetch(`${base(domain)}/permissions`);
  return (await res.json()) as PermissionPlan;
}

export async function applyPermissions(domain: string): Promise<PermissionPlan> {
  const res = await apiFetch(`${base(domain)}/permissions`, { method: 'POST' });
  return (await res.json()) as PermissionPlan;
}

// uploadFile goes through XHR rather than fetch because a site upload is large
// enough that an operator wants to see it move, and fetch has no upload
// progress. The CSRF header is set by hand for the same reason.
export function uploadFile(
  domain: string,
  path: string,
  file: File,
  onProgress?: (percent: number) => void
): Promise<FileResult> {
  return new Promise((resolve) => {
    const form = new FormData();
    form.append('path', path);
    form.append('file', file);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', apiUrl(`${base(domain)}/upload`));
    xhr.withCredentials = true;
    xhr.setRequestHeader('X-Servlo-CSRF', getCSRFToken());
    xhr.upload.onprogress = (e) => {
      if (onProgress && e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onerror = () => resolve({ ok: false, error: 'the upload could not be sent' });
    xhr.onload = () => {
      try {
        resolve(JSON.parse(xhr.responseText) as FileResult);
      } catch {
        resolve({ ok: false, error: xhr.responseText || `${xhr.status}` });
      }
    };
    xhr.send(form);
  });
}

// isArchive decides whether the Extract action is offered. By extension, which
// is what the operator sees; the server re-reads the file and refuses anything
// that is not really a zip.
export function isArchive(name: string): boolean {
  return name.toLowerCase().endsWith('.zip');
}

// languageForFile picks the editor's syntax highlighting from the extension.
//
// By name, because the server does not sniff the contents and guessing from
// bytes would be a second answer to a question the filename already answers.
// Anything unrecognised is plain text, which is the right thing for a log or a
// README and never wrong enough to matter.
const editorLanguages: Record<string, string> = {
  php: 'php',
  json: 'json',
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  css: 'css',
  scss: 'scss',
  html: 'html',
  htaccess: 'plaintext',
  xml: 'xml',
  yml: 'yaml',
  yaml: 'yaml',
  md: 'markdown',
  sql: 'sql',
  sh: 'shell',
  ini: 'ini',
  conf: 'nginx',
  lock: 'json'
};

export function languageForFile(path: string): string {
  const name = path.slice(path.lastIndexOf('/') + 1);
  // A dotenv file is ".env", ".env.production": the whole name, not a suffix.
  if (name === '.env' || name.startsWith('.env.')) return 'dotenv';
  const dot = name.lastIndexOf('.');
  if (dot <= 0) return 'plaintext';
  return editorLanguages[name.slice(dot + 1).toLowerCase()] ?? 'plaintext';
}

// parentOf is the path one level up, empty at the site root.
export function parentOf(path: string): string {
  const slash = path.lastIndexOf('/');
  return slash < 0 ? '' : path.slice(0, slash);
}

// breadcrumbs turns a path into the segments the header renders, each with the
// path that navigates to it.
export function breadcrumbs(path: string): Array<{ name: string; path: string }> {
  if (!path) return [];
  const parts = path.split('/');
  return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
}
