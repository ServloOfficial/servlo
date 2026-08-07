import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { get } from 'svelte/store';
import { lan, loadLANStatus, toggleLAN } from './lan';

describe('LAN store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    lan.update((state) => ({
      ...state,
      exposed: false,
      lanIP: '',
      loaded: false,
      loading: false,
      error: ''
    }));
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loads the exposure setting', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ exposed: true, lan_ip: '192.168.1.10', macos: false }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    ) as unknown as typeof fetch;

    await loadLANStatus();
    expect(get(lan)).toMatchObject({ exposed: true, lanIP: '192.168.1.10', loaded: true });
  });

  it('exposes through the LAN endpoint', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(JSON.parse(String(init?.body))).toEqual({ action: 'expose' });
      return new Response(
        '{"step":"Saving LAN exposure flag"}\n{"result":"ok","exposed":true,"lan_ip":"192.168.1.10"}\n',
        { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } }
      );
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    await toggleLAN('expose');

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(get(lan)).toMatchObject({ exposed: true, lanIP: '192.168.1.10', loading: false });
  });

  // Exposure covers nginx and nothing else. A state field saying a database is
  // reachable off the machine would describe something that cannot happen, so
  // the store carries no such field for a stale response to revive.
  it('carries no managed-service exposure state', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({ exposed: true, services_enabled: true, services_reachable: true }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    ) as unknown as typeof fetch;

    await loadLANStatus();
    const state = get(lan) as unknown as Record<string, unknown>;
    expect(state.servicesEnabled).toBeUndefined();
    expect(state.servicesReachable).toBeUndefined();
  });
});
