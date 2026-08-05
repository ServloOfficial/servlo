import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  diffSitesEvents,
  diffServicesEvents,
  diffUnhealthyEvents,
  now
} from './activity';
import type { Site } from './sites';
import type { Service } from './services';
import type { UnhealthyWorker } from './workerHealth';

function site(domain: string, extra: Partial<Site> = {}): Site {
  return { domain, ...extra };
}

function service(name: string, extra: Partial<Service> = {}): Service {
  return { name, status: 'active', site_count: 0, ...extra };
}

function unhealthy(unit: string, site: string, worker: string): UnhealthyWorker {
  return { unit, site, worker, state: 'failed' };
}

describe('diffSitesEvents', () => {
  it('returns empty when prev is null (initial hydration is silent)', () => {
    expect(diffSitesEvents(null, [site('a.test')])).toEqual([]);
  });

  it('emits site_linked for new domains', () => {
    const prev = new Map<string, Site>([['a.test', site('a.test')]]);
    const events = diffSitesEvents(prev, [site('a.test'), site('b.test')]);
    expect(events).toEqual([{ kind: 'site_linked', subject: 'b.test' }]);
  });

  it('emits site_removed for deleted domains', () => {
    const prev = new Map<string, Site>([
      ['a.test', site('a.test')],
      ['b.test', site('b.test')]
    ]);
    const events = diffSitesEvents(prev, [site('a.test')]);
    expect(events).toEqual([{ kind: 'site_removed', subject: 'b.test' }]);
  });

  it('emits site_paused / site_resumed on pause flag change', () => {
    const prev = new Map<string, Site>([['a.test', site('a.test', { paused: false })]]);
    expect(diffSitesEvents(prev, [site('a.test', { paused: true })])).toEqual([
      { kind: 'site_paused', subject: 'a.test' }
    ]);
    const prev2 = new Map<string, Site>([['a.test', site('a.test', { paused: true })]]);
    expect(diffSitesEvents(prev2, [site('a.test', { paused: false })])).toEqual([
      { kind: 'site_resumed', subject: 'a.test' }
    ]);
  });

  it('emits site_running / site_stopped on fpm flip while not paused', () => {
    const prev = new Map<string, Site>([['a.test', site('a.test', { fpm_running: false })]]);
    expect(diffSitesEvents(prev, [site('a.test', { fpm_running: true })])).toEqual([
      { kind: 'site_running', subject: 'a.test' }
    ]);
  });

  it('does not emit running/stopped when site is paused', () => {
    const prev = new Map<string, Site>([
      ['a.test', site('a.test', { fpm_running: true, paused: false })]
    ]);
    const events = diffSitesEvents(prev, [
      site('a.test', { fpm_running: false, paused: true })
    ]);
    // pause toggle is emitted, but stopped is not (it's a side-effect of the pause)
    expect(events).toEqual([{ kind: 'site_paused', subject: 'a.test' }]);
  });

});

describe('diffServicesEvents', () => {
  it('returns empty when prev is null', () => {
    expect(diffServicesEvents(null, [service('mysql')])).toEqual([]);
  });

  it('emits service_active / service_inactive on status flip', () => {
    const prev = new Map<string, Service>([['mysql', service('mysql', { status: 'inactive' })]]);
    expect(diffServicesEvents(prev, [service('mysql', { status: 'active' })])).toEqual([
      { kind: 'service_active', subject: 'mysql' }
    ]);
  });

  it('emits service_update when update_available goes false → true', () => {
    const prev = new Map<string, Service>([
      ['mysql', service('mysql', { update_available: false })]
    ]);
    const events = diffServicesEvents(prev, [
      service('mysql', { update_available: true, latest_version: '8.5' })
    ]);
    expect(events).toEqual([
      { kind: 'service_update', subject: 'mysql', meta: { version: '8.5' } }
    ]);
  });

  it('emits service_added when a non-worker service appears', () => {
    const prev = new Map<string, Service>();
    expect(diffServicesEvents(prev, [service('mysql')])).toEqual([
      { kind: 'service_added', subject: 'mysql' }
    ]);
  });

  it('does not emit service_added for worker services', () => {
    const prev = new Map<string, Service>();
    const worker = service('queue-foo', { queue_site: 'foo' });
    expect(diffServicesEvents(prev, [worker])).toEqual([]);
  });

  it('emits service_removed when a non-worker service disappears', () => {
    const prev = new Map<string, Service>([['mysql', service('mysql')]]);
    expect(diffServicesEvents(prev, [])).toEqual([
      { kind: 'service_removed', subject: 'mysql' }
    ]);
  });

  it('does not emit service_removed for worker services', () => {
    const prev = new Map<string, Service>([
      ['queue-foo', service('queue-foo', { queue_site: 'foo' })]
    ]);
    expect(diffServicesEvents(prev, [])).toEqual([]);
  });

  it('emits service_version on version bump', () => {
    const prev = new Map<string, Service>([
      ['mysql', service('mysql', { version: 'v8.4' })]
    ]);
    expect(diffServicesEvents(prev, [service('mysql', { version: 'v8.5' })])).toEqual([
      { kind: 'service_version', subject: 'mysql', meta: { version: 'v8.5' } }
    ]);
  });
});


describe('diffUnhealthyEvents', () => {
  it('returns empty when prev is null', () => {
    expect(diffUnhealthyEvents(null, [unhealthy('servlo-queue-foo', 'foo.test', 'queue')])).toEqual([]);
  });

  it('emits worker_failed for new units', () => {
    const prev = new Set<string>();
    const events = diffUnhealthyEvents(prev, [unhealthy('servlo-queue-foo', 'foo.test', 'queue')]);
    expect(events).toEqual([
      { kind: 'worker_failed', subject: 'foo.test', meta: { worker: 'queue' } }
    ]);
  });

  it('emits worker_healed when a unit drops out', () => {
    const prev = new Set<string>(['servlo-queue-foo']);
    const events = diffUnhealthyEvents(prev, []);
    expect(events).toEqual([{ kind: 'worker_healed', subject: 'servlo-queue-foo' }]);
  });

  it('emits nothing when set is unchanged', () => {
    const prev = new Set<string>(['servlo-queue-foo']);
    const events = diffUnhealthyEvents(prev, [unhealthy('servlo-queue-foo', 'foo.test', 'queue')]);
    expect(events).toEqual([]);
  });
});

describe('now clock', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // The clock only exists to re-render relative timestamps in the activity
  // list. With no subscriber there is nothing on screen to re-render, so the
  // interval must be armed on first subscribe and cleared on last unsubscribe.
  // Armed at module scope instead, it runs for the life of the page whichever
  // view is open, and nothing can ever stop it.
  it('arms the interval on subscribe and clears it on unsubscribe', () => {
    vi.useFakeTimers();
    const idle = vi.getTimerCount();

    const unsub = now.subscribe(() => {});
    expect(vi.getTimerCount()).toBe(idle + 1);

    unsub();
    expect(vi.getTimerCount()).toBe(idle);
  });

  it('ticks while subscribed and stops once the last subscriber leaves', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(1_000_000));

    const seen: number[] = [];
    const unsub = now.subscribe((v) => seen.push(v));
    const initial = seen.length;

    vi.setSystemTime(new Date(1_060_000));
    vi.advanceTimersByTime(60_000);
    expect(seen.length).toBeGreaterThan(initial);

    unsub();
    const afterUnsub = seen.length;
    vi.setSystemTime(new Date(1_200_000));
    vi.advanceTimersByTime(120_000);
    expect(seen.length).toBe(afterUnsub);
  });

  // The dashboard tab unmounts when another tab is open, so the clock stops
  // and holds whatever it read last. Without a read on subscribe the list
  // renders against that frozen value for up to 30s on the way back, ageing
  // every event by however long the user was away.
  it('reads the wall clock on subscribe, not the value it froze at', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(1_000_000));
    now.subscribe(() => {})();

    vi.setSystemTime(new Date(1_000_000 + 20 * 60_000));
    let seen = 0;
    now.subscribe((v) => (seen = v))();

    expect(seen).toBe(Date.now());
  });
});
