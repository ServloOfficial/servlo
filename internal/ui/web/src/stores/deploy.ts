import { m } from '../paraglide/messages.js';
import { apiFetch } from '$lib/api';
import { readSSE } from '$lib/sse';

const site = (domain: string, action: string) =>
  '/api/sites/' + encodeURIComponent(domain) + '/' + action;

interface ActionResult {
  ok: boolean;
  error?: string;
}

async function postJSON(path: string, body: unknown): Promise<ActionResult> {
  try {
    const res = await apiFetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = (await res.json()) as ActionResult;
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

// The site's deploy script. `exists` is false while the site is still on its
// framework's template, which the editor says out loud: an operator looking at
// a Laravel script needs to know whether it is the one that will run or the one
// servlo suggests.
export interface SiteDeployScript {
  path: string;
  body: string;
  exists: boolean;
  // Whether this script triggers the pre-deploy database backup, which is the
  // difference between a deploy that can be undone and one that cannot.
  migrates: boolean;
}

export async function loadSiteDeployScript(domain: string): Promise<SiteDeployScript> {
  const res = await apiFetch(site(domain, 'deploy-script'));
  if (!res.ok) throw new Error(m.common_requestFailed());
  return (await res.json()) as SiteDeployScript;
}

export function saveSiteDeployScript(domain: string, body: string): Promise<ActionResult> {
  return postJSON(site(domain, 'deploy-script'), { body, backup: true });
}

// The paths a deploy must not remove. `custom` is false while the site is
// following its framework, and `default` is what resetting restores.
export interface SiteDeployExclude {
  paths: string[];
  custom: boolean;
  default: string[];
}

export async function loadSiteDeployExclude(domain: string): Promise<SiteDeployExclude> {
  const res = await apiFetch(site(domain, 'deploy-exclude'));
  if (!res.ok) throw new Error(m.common_requestFailed());
  return (await res.json()) as SiteDeployExclude;
}

export function saveSiteDeployExclude(domain: string, paths: string[]): Promise<ActionResult> {
  return postJSON(site(domain, 'deploy-exclude'), { paths });
}

// Resetting is not the same as saving an empty list, and the two have to stay
// distinguishable all the way to the server: an empty list protects nothing,
// and following the framework protects whatever it declares.
export function resetSiteDeployExclude(domain: string): Promise<ActionResult> {
  return postJSON(site(domain, 'deploy-exclude'), { reset: true });
}

// What a finished deploy reports. The commits are here on failure too: a deploy
// that pulled and then failed its script left the site on code that was never
// prepared, and that is the most important thing to know from the panel.
export interface DeployDone {
  ok: boolean;
  error?: string;
  from?: string;
  to?: string;
  snapshot?: string;
  duration_ms?: number;
}

export type DeployEvent = { line: string } | ({ done: true } & DeployDone);

// A deploy takes minutes, so its output streams rather than arriving at the
// end. Same SSE shape as the command runner and the PHP build.
export async function streamDeploy(
  domain: string,
  onEvent: (e: DeployEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await apiFetch(site(domain, 'deploy'), { method: 'POST', signal });
  const ct = res.headers.get('Content-Type') || '';
  if (!ct.startsWith('text/event-stream')) {
    // A refusal answers as JSON before the stream starts, which is how "this
    // site is busy running migrate" arrives.
    const data = (await res.json().catch(() => ({}))) as { error?: string };
    onEvent({ done: true, ok: false, error: data.error ?? m.common_requestFailed() });
    return;
  }
  await readSSE(res, (event, data) => {
    if (event !== 'done') {
      onEvent({ line: data });
      return;
    }
    try {
      const r = JSON.parse(data) as DeployDone;
      onEvent({ done: true, ...r, ok: Boolean(r.ok) });
    } catch {
      onEvent({ done: true, ok: false, error: m.common_requestFailed() });
    }
  });
}
