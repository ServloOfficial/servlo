import { describe, it, expect, vi, afterEach } from 'vitest';
import { apiUrl, wsUrl, apiFetch, setCSRFToken } from './api';

describe('apiUrl', () => {
  it('passes absolute URLs through', () => {
    expect(apiUrl('https://example.com/foo')).toBe('https://example.com/foo');
    expect(apiUrl('http://x/y')).toBe('http://x/y');
  });

  it('prepends apiBase only when non-empty', () => {
    // default hostname in jsdom is "localhost", so apiBase is ''
    expect(apiUrl('/api/version')).toBe('/api/version');
  });
});

describe('apiFetch CSRF header', () => {
  afterEach(() => vi.unstubAllGlobals());

  function stubFetch() {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('', { status: 200 })
    );
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
  }

  // The header used to carry a placeholder, and its presence was the proof.
  // It carries the session's token now: a marker anyone can set is not a token.
  it('adds the session CSRF token to state-changing requests', async () => {
    setCSRFToken('the-token');
    const fetchMock = stubFetch();
    await apiFetch('/api/sites/x/restart', { method: 'POST' });
    const sent = new Headers(fetchMock.mock.calls[0][1]?.headers);
    expect(sent.get('X-Servlo-CSRF')).toBe('the-token');
  });

  it('leaves GET and HEAD untouched', async () => {
    for (const method of ['GET', 'HEAD', undefined]) {
      const fetchMock = stubFetch();
      await apiFetch('/api/sites', method ? { method } : undefined);
      const sent = new Headers(fetchMock.mock.calls[0][1]?.headers);
      expect(sent.has('X-Servlo-CSRF')).toBe(false);
    }
  });

  it('preserves caller-supplied headers and CSRF override', async () => {
    const fetchMock = stubFetch();
    await apiFetch('/api/services/redis/restart', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Servlo-CSRF': 'custom' }
    });
    const sent = new Headers(fetchMock.mock.calls[0][1]?.headers);
    expect(sent.get('Content-Type')).toBe('application/json');
    expect(sent.get('X-Servlo-CSRF')).toBe('custom');
  });
});

describe('wsUrl', () => {
  it('rewrites http to ws', () => {
    const u = wsUrl('/api/ws');
    expect(u.startsWith('ws://') || u.startsWith('wss://')).toBe(true);
    expect(u.endsWith('/api/ws')).toBe(true);
  });
});
