import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';
import {
  dbConnections,
  loadDBConnections,
  addConnection,
  removeConnection,
  setDefaultConnection,
  assignConnection,
  connectionLocation,
  connectionForSite,
  connectionMeta,
  siteCountLabel,
  testConnection,
  isManagedConnection,
  type DBConnection
} from './dbConnections';

const managed: DBConnection = {
  name: 'managed',
  family: 'postgres',
  service: '',
  host: 'db.example.net',
  port: 25060,
  user: 'doadmin',
  tls_mode: 'require',
  ca_cert: '',
  default: true,
  sites: ['shop.example']
};

const local: DBConnection = {
  name: 'local',
  family: 'mysql',
  service: 'mysql',
  host: 'servlo-mysql',
  port: 3306,
  user: 'root',
  default: false,
  sites: []
};

describe('dbConnections store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    dbConnections.set({
      connections: [],
      services: [],
      serverIPs: [],
      serverIPsError: '',
      loading: false,
      error: ''
    });
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loadDBConnections maps the response into the store', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ connections: [managed], services: ['mysql', 'postgres'], error: '' }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;
    await loadDBConnections();
    const state = get(dbConnections);
    expect(state.loading).toBe(false);
    expect(state.error).toBe('');
    expect(state.services).toEqual(['mysql', 'postgres']);
    expect(state.connections).toHaveLength(1);
    expect(state.connections[0].name).toBe('managed');
    expect(state.connections[0].sites).toEqual(['shop.example']);
    expect(connectionLocation(state.connections[0])).toBe('db.example.net:25060');
  });

  it('addConnection POSTs the managed payload and takes the list from the reply', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response(
        JSON.stringify({
          ok: true,
          connections: { connections: [managed, local], services: ['mysql'] }
        }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    const res = await addConnection({
      name: 'managed',
      engine: 'postgres',
      host: 'db.example.net',
      port: 25060,
      user: 'doadmin',
      password: 's3cret',
      tls_mode: 'require',
      ca_cert_pem: '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n'
    });
    expect(res.ok).toBe(true);
    // One request: the action's own reply is where the next state comes from.
    expect(calls).toHaveLength(1);
    expect(calls[0][0]).toBe('/api/db-connections');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({
      action: 'add',
      name: 'managed',
      engine: 'postgres',
      host: 'db.example.net',
      port: 25060,
      user: 'doadmin',
      password: 's3cret',
      tls_mode: 'require',
      ca_cert_pem: '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n'
    });
    expect(get(dbConnections).connections.map((c) => c.name)).toEqual(['managed', 'local']);
  });

  it('addConnection POSTs a local connection as a service name', async () => {
    const bodies: string[] = [];
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(
        JSON.stringify({ ok: true, connections: { connections: [local], services: ['mysql'] } }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    await addConnection({ name: 'local', service: 'mysql' });
    expect(JSON.parse(bodies[0])).toEqual({ action: 'add', name: 'local', service: 'mysql' });
  });

  it('removeConnection surfaces the backend error verbatim and leaves the list alone', async () => {
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      if ((init?.method ?? 'GET') === 'GET') {
        return new Response(JSON.stringify({ connections: [managed], services: [] }), {
          status: 200
        });
      }
      return new Response(
        JSON.stringify({ error: 'these sites are on managed: shop.example. Move them first.' }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    await loadDBConnections();
    const res = await removeConnection('managed');
    expect(res.ok).toBe(false);
    expect(res.error).toBe('these sites are on managed: shop.example. Move them first.');
    expect(get(dbConnections).connections.map((c) => c.name)).toEqual(['managed']);
  });

  it('setDefaultConnection POSTs the default action', async () => {
    const bodies: string[] = [];
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(
        JSON.stringify({ ok: true, connections: { connections: [local], services: [] } }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    await setDefaultConnection('local');
    expect(JSON.parse(bodies[0])).toEqual({ action: 'default', name: 'local' });
  });

  it('assignConnection POSTs the domain and the connection', async () => {
    const bodies: string[] = [];
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(
        JSON.stringify({ ok: true, connections: { connections: [managed], services: [] } }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    const res = await assignConnection('shop.example', 'managed');
    expect(res.ok).toBe(true);
    expect(JSON.parse(bodies[0])).toEqual({
      action: 'assign',
      domain: 'shop.example',
      connection: 'managed'
    });
    expect(connectionForSite(get(dbConnections).connections, 'shop.example')).toBe('managed');
  });

  it('siteCountLabel counts one site in the singular', () => {
    expect(siteCountLabel(1)).toBe('1 site');
    expect(siteCountLabel(0)).toBe('0 sites');
    expect(siteCountLabel(3)).toBe('3 sites');
  });

  it('connectionMeta names a local connection once and skips its address', () => {
    const meta = connectionMeta({ ...local, sites: ['a.example', 'b.example'] });
    expect(meta.engine).toBe('MySQL');
    expect(meta.location).toBe('');
    expect(meta.kind).toBe('local service');
    expect(meta.sites).toBe('2 sites');
  });

  it('connectionMeta keeps the dialect and the address for a managed connection', () => {
    const meta = connectionMeta(managed);
    expect(meta.engine).toBe('PostgreSQL');
    expect(meta.location).toBe('db.example.net:25060');
    expect(meta.kind).toBe('Managed');
    expect(meta.sites).toBe('1 site');
  });

  it('loadDBConnections keeps this server\'s address for the trusted-sources note', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ connections: [managed], services: [], server_ips: ['203.0.113.10'] }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;
    await loadDBConnections();
    expect(get(dbConnections).serverIPs).toEqual(['203.0.113.10']);
    expect(get(dbConnections).serverIPsError).toBe('');
  });

  it('loadDBConnections keeps the reason the address could not be worked out', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            connections: [],
            services: [],
            server_ips: [],
            server_ips_error: 'no public address on any interface'
          }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;
    await loadDBConnections();
    expect(get(dbConnections).serverIPsError).toBe('no public address on any interface');
  });

  it('testConnection POSTs the test action and carries the failure back', async () => {
    const bodies: string[] = [];
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(
        JSON.stringify({ error: 'cannot reach db.example.net:25060: i/o timeout' }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    const res = await testConnection('managed');
    expect(JSON.parse(bodies[0])).toEqual({ action: 'test', name: 'managed' });
    expect(res.ok).toBe(false);
    expect(res.error).toBe('cannot reach db.example.net:25060: i/o timeout');
  });

  it('only a managed connection is one servlo reaches over the network', () => {
    expect(isManagedConnection(managed)).toBe(true);
    expect(isManagedConnection(local)).toBe(false);
  });

  it('assignConnection sends an empty connection for the install default', async () => {
    const bodies: string[] = [];
    globalThis.fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(
        JSON.stringify({ ok: true, connections: { connections: [], services: [] } }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    await assignConnection('shop.example', '');
    expect(JSON.parse(bodies[0])).toEqual({
      action: 'assign',
      domain: 'shop.example',
      connection: ''
    });
    expect(connectionForSite(get(dbConnections).connections, 'shop.example')).toBe('');
  });
});
