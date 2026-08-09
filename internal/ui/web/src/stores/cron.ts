import { m } from '../paraglide/messages.js';
import { apiFetch } from '$lib/api';

const sitePath = (domain: string, action: string) =>
  '/api/sites/' + encodeURIComponent(domain) + '/' + action;

export interface ActionResult {
  ok: boolean;
  error?: string;
}

/** What happened the last time an entry ran, read back from systemd rather than
 *  remembered by the panel, so it survives a restart of either. */
export interface CronRun {
  at: string;
  ok: boolean;
  running: boolean;
  exit_code: number;
  result?: string;
  output?: string[];
}

/** One scheduled command and its state. `last_run` is absent for an entry that
 *  has never run, which is a different thing from one that failed. */
export interface CronEntry {
  id: string;
  name: string;
  command: string;
  /** The schedule as it was typed. */
  schedule: string;
  /** The systemd OnCalendar expression it became. */
  calendar: string;
  capture_output: boolean;
  disabled: boolean;
  /** True for an entry servlo installed on the framework's behalf, which has
   *  its own switch rather than being hand-edited. */
  managed?: boolean;
  unit: string;
  next_run?: string;
  last_run?: CronRun;
}

/** The framework's own page-load scheduler, when it has one. */
export interface PseudoCron {
  available: boolean;
  label?: string;
  description?: string;
  replaced: boolean;
  schedule?: string;
  command?: string;
  constant?: string;
  file?: string;
}

export interface SiteCron {
  entries: CronEntry[];
  pseudo_cron: PseudoCron;
  supported: boolean;
  unsupported?: string;
}

export interface CronSave {
  ok: boolean;
  error?: string;
  entry?: CronEntry;
}

export async function loadSiteCron(domain: string): Promise<SiteCron> {
  const res = await apiFetch(sitePath(domain, 'cron'));
  if (!res.ok) throw new Error(m.common_requestFailed());
  return (await res.json()) as SiteCron;
}

export interface CronDraft {
  id?: string;
  name: string;
  command: string;
  schedule: string;
  capture_output: boolean;
  disabled: boolean;
}

export async function saveSiteCron(domain: string, entry: CronDraft): Promise<CronSave> {
  try {
    const res = await apiFetch(sitePath(domain, 'cron'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(entry)
    });
    const data = (await res.json()) as CronSave;
    return { ok: Boolean(data.ok), error: data.error, entry: data.entry };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

export async function deleteSiteCron(domain: string, id: string): Promise<ActionResult> {
  try {
    const res = await apiFetch(sitePath(domain, 'cron') + '/' + encodeURIComponent(id), {
      method: 'DELETE'
    });
    const data = (await res.json()) as ActionResult;
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

/** Switch the framework's page-load scheduler for a real timer, or put it back.
 *  Both directions matter: turning it off has to restore the framework's own
 *  scheduler, not leave the site with nothing running its scheduled work. */
export async function setPseudoCron(domain: string, replace: boolean): Promise<ActionResult> {
  try {
    const res = await apiFetch(sitePath(domain, 'pseudo-cron'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ replace })
    });
    const data = (await res.json()) as ActionResult;
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}
