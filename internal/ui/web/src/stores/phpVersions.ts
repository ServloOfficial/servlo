import { m } from '../paraglide/messages.js';
import { writable } from 'svelte/store';
import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import { readSSE } from '$lib/sse';
import type { SiteNginxBackup, LoadNginxBackupsResult, ResetNginxResult, SaveNginxResult, RestoreNginxResult } from './sites';

export const phpVersions = writable<string[]>([]);

export interface PhpOption {
  value: string;
  disabled?: boolean;
  description?: string;
}

// cmpVersion compares two "major.minor" strings numerically.
function cmpVersion(a: string, b: string): number {
  const [aMaj, aMin] = a.split('.').map((n) => parseInt(n, 10) || 0);
  const [bMaj, bMin] = b.split('.').map((n) => parseInt(n, 10) || 0);
  return aMaj !== bMaj ? aMaj - bMaj : aMin - bMin;
}

// outOfFrameworkRange reports whether a PHP version falls outside the
// framework's [min, max] range. An empty bound means unconstrained on that side.
function outOfFrameworkRange(v: string, min?: string, max?: string): boolean {
  if (min && cmpVersion(v, min) < 0) return true;
  if (max && cmpVersion(v, max) > 0) return true;
  return false;
}

// phpOptionsForSite returns the options to offer in a site's PHP dropdown.
// FrankenPHP sites are limited to the versions dunglas/frankenphp publishes an
// image for, intersected with what's installed, plus the site's current version
// so the control never renders blank. Other runtimes offer every installed one.
// When the framework declares a PHP range (min/max), versions outside it are
// kept in the list but disabled, so the constraint is visible rather than
// silently hidden. The site's current version is never disabled. min/max are
// empty when the framework version was guessed, leaving every version enabled.
export function phpOptionsForSite(
  runtime: string | undefined,
  installed: string[],
  frankenphpVersions: string[],
  current: string,
  min?: string,
  max?: string
): PhpOption[] {
  const base =
    runtime === 'frankenphp'
      ? frankenphpVersions.filter((v) => installed.includes(v) || v === current)
      : installed;
  return base.map((v) => {
    const disabled = v !== current && outOfFrameworkRange(v, min, max);
    return disabled
      ? { value: v, disabled: true, description: `needs PHP ${min || '*'} to ${max || '*'}` }
      : { value: v };
  });
}

export async function loadPhpVersions() {
  try {
    const list = await apiJson<string[]>('/api/php-versions');
    phpVersions.set(Array.isArray(list) ? list : []);
  } catch {
    /* keep previous */
  }
}

// installablePhpVersions returns the supported PHP versions not yet installed,
// so the add-version modal can offer them in a dropdown. Throws on request
// failure so the caller can distinguish an error from an empty (all-installed)
// list rather than silently showing "all installed".
export async function installablePhpVersions(): Promise<string[]> {
  const list = await apiJson<string[]>('/api/php-installable');
  return Array.isArray(list) ? list : [];
}

export interface PhpInstallEvent {
  line?: string;
  done?: boolean;
  ok?: boolean;
  version?: string;
  error?: string;
}

// streamPhpInstall POSTs to the SSE endpoint and invokes onEvent for each build
// log line and the final done payload. Mirrors streamWorktreeAdd. Pass a signal
// to abort the client read when the modal closes; the server build continues and
// reports its result via a push notification.
export async function streamPhpInstall(
  version: string,
  onEvent: (e: PhpInstallEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await apiFetch('/api/php-versions/install?version=' + encodeURIComponent(version), {
    method: 'POST',
    signal
  });
  await readSSE(res, (event, data) => {
    if (event === 'done') {
      try {
        const r = JSON.parse(data) as { ok?: boolean; version?: string; error?: string };
        onEvent({ done: true, ok: Boolean(r.ok), version: r.version, error: r.error });
      } catch {
        onEvent({ done: true, ok: false, error: 'bad done payload' });
      }
    } else {
      onEvent({ line: data });
    }
  });
}

// streamPhpRebuild force-rebuilds a version's image against the current base,
// streaming the build log the same way an install does.
export async function streamPhpRebuild(
  version: string,
  onEvent: (e: PhpInstallEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await apiFetch('/api/php-versions/' + encodeURIComponent(version) + '/rebuild', {
    method: 'POST',
    signal
  });
  await readSSE(res, (event, data) => {
    if (event === 'done') {
      try {
        const r = JSON.parse(data) as { ok?: boolean; version?: string; error?: string };
        onEvent({ done: true, ok: Boolean(r.ok), version: r.version, error: r.error });
      } catch {
        onEvent({ done: true, ok: false, error: 'bad done payload' });
      }
    } else {
      onEvent({ line: data });
    }
  });
}

// BaseImageStatus compares a version's image against the prebuilt base it was
// built from. null when there is nothing to report: no recorded base (a local
// build) or a registry that could not answer.
export interface BaseImageStatus {
  version: string;
  ref?: string;
  built_digest?: string;
  latest_digest?: string;
  stale: boolean;
}

// checkPhpUpdates forces a fresh registry read for a version's base image,
// bypassing the digest cache. ok is false when the request itself failed.
export async function checkPhpUpdates(
  version: string
): Promise<{ ok: boolean; status: BaseImageStatus | null }> {
  try {
    const res = await apiFetch('/api/php-versions/' + encodeURIComponent(version) + '/updates', {
      method: 'POST'
    });
    if (!res.ok) return { ok: false, status: null };
    const data = (await res.json().catch(() => null)) as BaseImageStatus | null;
    return { ok: true, status: data };
  } catch {
    return { ok: false, status: null };
  }
}

async function phpAction(v: string, action: 'set-default' | 'start' | 'stop' | 'remove'): Promise<boolean> {
  try {
    const res = await apiFetch('/api/php-versions/' + encodeURIComponent(v) + '/' + action, {
      method: 'POST'
    });
    return res.ok;
  } catch {
    return false;
  }
}

export const setDefaultPhp = (v: string) => phpAction(v, 'set-default');
export const startPhp = (v: string) => phpAction(v, 'start');
export const stopPhp = (v: string) => phpAction(v, 'stop');
export const removePhp = (v: string) => phpAction(v, 'remove');

// setFpmPorts replaces the extra host ports published on a version's shared FPM
// container. The server shifts any colliding host port to the next free one, so
// the returned list may differ from what was sent; the status broadcast carries
// the resolved set, so the caller reseeds from it rather than the response.
export async function setFpmPorts(
  version: string,
  ports: string[]
): Promise<{ ok: boolean; error?: string; ports?: string[] }> {
  try {
    const res = await apiFetch('/api/php-versions/' + encodeURIComponent(version) + '/ports', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ports })
    });
    const data = (await res.json().catch(() => ({}))) as {
      ok?: boolean;
      error?: string;
      ports?: string[];
    };
    if (res.ok && data.ok) return { ok: true, ports: data.ports };
    return { ok: false, error: data.error || m.common_failed() };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

export interface PhpSetState {
  declared: string[];
  has: string[] | null;
  cannot: string[] | null;
}

export interface PhpExtensionsReport {
  version: string;
  built: boolean;
  needs_rebuild: boolean;
  extensions: PhpSetState;
  packages: PhpSetState;
  modules?: string[];
}

export interface PhpExtensionsResult {
  ok: boolean;
  error?: string;
  report?: PhpExtensionsReport;
  modules_error?: string;
}

// fetchPhpExtensions reads what a version's image actually carries. Reading
// `php -m` starts a container, so this is only called when the tab is opened;
// the backend caches the result against the image ID.
export async function fetchPhpExtensions(v: string): Promise<PhpExtensionsResult> {
  try {
    const res = await apiFetch('/api/php-versions/' + encodeURIComponent(v) + '/extensions');
    const data = await decodeJSONResult<PhpExtensionsResult>(res);
    return { ok: Boolean(data.ok), error: data.error, report: data.report, modules_error: data.modules_error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}
