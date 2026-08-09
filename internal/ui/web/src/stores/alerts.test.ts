import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { alerts, alertsLoaded, criticalAlerts, loadAlerts, dismissAlert } from './alerts';

function respond(body: unknown, ok = true) {
  return Promise.resolve({ ok, json: () => Promise.resolve(body) } as Response);
}

const down = { kind: 'site_down', site: 'acme', title: 'Site is down', message: '502', at: '2026-08-09T10:00:00Z' };
const disk = { kind: 'disk_filling', title: 'Disk is filling up', message: '94%', at: '2026-08-09T09:00:00Z' };

describe('the alerts store', () => {
  beforeEach(() => {
    alerts.set([]);
    alertsLoaded.set(false);
    vi.restoreAllMocks();
  });

  it('loads what is currently wrong', async () => {
    vi.stubGlobal('fetch', vi.fn(() => respond({ alerts: [down, disk] })));
    await loadAlerts();
    expect(get(alerts)).toHaveLength(2);
    expect(get(alertsLoaded)).toBe(true);
  });

  // A panel that cannot reach its own API has a bigger problem than the alert
  // list, and showing a stale list is better than showing an empty one that
  // reads as good news.
  it('keeps what it had when the request fails, and stops waiting', async () => {
    alerts.set([down]);
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('offline'))));
    await loadAlerts();
    expect(get(alerts)).toHaveLength(1);
    expect(get(alertsLoaded)).toBe(true);
  });

  // The dashboard turns the card red for these and amber for the rest, so an
  // outage does not look the same as a disk that will be full next week.
  it('separates the ones that mean visitors are seeing nothing', () => {
    alerts.set([down, disk]);
    expect(get(criticalAlerts).map((a) => a.kind)).toEqual(['site_down']);
  });

  it('replaces the list with what the server says is left after a dismissal', async () => {
    alerts.set([down, disk]);
    vi.stubGlobal('fetch', vi.fn(() => respond({ alerts: [disk] })));
    expect(await dismissAlert('site_down', 'acme')).toBeNull();
    expect(get(alerts).map((a) => a.kind)).toEqual(['disk_filling']);
  });

  // A dismissal the server refused must not take the row off screen: the alert
  // is still open, and a card that disagrees with the server is worse than one
  // that shows an error.
  it('puts the list back when the server refuses', async () => {
    alerts.set([down, disk]);
    vi.stubGlobal('fetch', vi.fn(() => respond({ alerts: [], error: 'which alert?' })));
    expect(await dismissAlert('', undefined)).toBe('which alert?');
    expect(get(alerts)).toHaveLength(2);
  });

  it('puts the list back when the request itself fails', async () => {
    alerts.set([down, disk]);
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('offline'))));
    expect(await dismissAlert('site_down', 'acme')).toContain('offline');
    expect(get(alerts)).toHaveLength(2);
  });
});
