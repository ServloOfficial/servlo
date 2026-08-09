import { describe, it, expect, vi, beforeEach } from 'vitest';
import { loadSiteDBUser, rotateSiteDBUser } from './dbUser';

function respond(body: unknown, ok = true) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status: ok ? 200 : 500,
      headers: { 'content-type': 'application/json' }
    })
  );
}

describe('dbUser store', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('reads the account for a site', async () => {
    const fetchMock = vi.fn((_url: RequestInfo | URL) =>
      respond({ connection: 'mysql', location: 'servlo-mysql', user: 'acme', own_account: true })
    );
    vi.stubGlobal('fetch', fetchMock);

    const account = await loadSiteDBUser('acme.example');

    expect(account?.user).toBe('acme');
    expect(account?.own_account).toBe(true);
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/sites/acme.example/db-user');
  });

  // A domain goes into a URL path, so it is encoded rather than pasted.
  it('encodes the domain', async () => {
    const fetchMock = vi.fn((_url: RequestInfo | URL) => respond({}));
    vi.stubGlobal('fetch', fetchMock);

    await rotateSiteDBUser('a b/c');

    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/sites/a%20b%2Fc/db-user/rotate');
  });

  it('rotates and reports the keys that were rewritten', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => respond({ ok: true, user: 'acme', env_keys: ['DB_PASSWORD', 'DB_USERNAME'] }))
    );

    const res = await rotateSiteDBUser('acme.example');

    expect(res.ok).toBe(true);
    expect(res.env_keys).toEqual(['DB_PASSWORD', 'DB_USERNAME']);
  });

  // The one failure worth spelling out: the server has the new password and the
  // env file does not. The message has to reach the card, not be flattened.
  it('carries the server error through', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => respond({ error: 'could not write .env' }, false))
    );

    const res = await rotateSiteDBUser('acme.example');

    expect(res.ok).toBe(false);
    expect(res.error).toContain('could not write .env');
  });
});
