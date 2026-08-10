import { writable, derived, get } from 'svelte/store';
import { apiFetch } from '$lib/api';

export interface PanelAlert {
  kind: string;
  site?: string;
  message: string;
  at: string;
  /** The heading, resolved by the server so the panel and the emails say the
   *  same words and there is one place to change them. */
  title: string;
}

interface AlertsResponse {
  alerts: PanelAlert[];
  error?: string;
}

export const alerts = writable<PanelAlert[]>([]);
export const alertsLoaded = writable(false);

/** Which of the six are worth interrupting somebody over. A worker down or a
 *  backup that failed is bad and not yet an outage; a site that is down is
 *  visitors seeing nothing. */
const critical = new Set(['site_down', 'cert_renew_failed']);
export const criticalAlerts = derived(alerts, ($a) => $a.filter((x) => critical.has(x.kind)));

export async function loadAlerts(): Promise<void> {
  try {
    const res = await apiFetch('/api/alerts');
    if (!res.ok) return;
    const body = (await res.json()) as AlertsResponse;
    alerts.set(body.alerts ?? []);
  } catch {
    // A panel that cannot reach its own API has a bigger problem than the
    // alert list, and it is already reporting that one.
  } finally {
    alertsLoaded.set(true);
  }
}

export async function dismissAlert(kind: string, site?: string): Promise<string | null> {
  const before = get(alerts);
  try {
    const res = await apiFetch('/api/alerts', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ kind, site: site ?? '' })
    });
    const body = (await res.json()) as AlertsResponse;
    if (body.error) {
      alerts.set(before);
      return body.error;
    }
    alerts.set(body.alerts ?? []);
    return null;
  } catch (e) {
    alerts.set(before);
    return String(e);
  }
}
