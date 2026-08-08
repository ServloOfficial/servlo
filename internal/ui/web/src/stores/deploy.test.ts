import { describe, it, expect, vi, beforeEach } from 'vitest';

const apiFetch = vi.fn();
vi.mock('$lib/api', () => ({ apiFetch: (...a: unknown[]) => apiFetch(...(a as [])) }));

import { streamDeploy, saveSiteDeployExclude, resetSiteDeployExclude } from './deploy';

function sse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  return {
    ok: true,
    headers: { get: () => 'text/event-stream' },
    body: {
      getReader() {
        let i = 0;
        return {
          read: async () =>
            i < chunks.length
              ? { value: encoder.encode(chunks[i++]), done: false }
              : { value: undefined, done: true }
        };
      }
    }
  } as unknown as Response;
}

beforeEach(() => apiFetch.mockReset());

describe('streamDeploy', () => {
  it('reports every line and then what the deploy did', async () => {
    apiFetch.mockResolvedValue(
      sse([
        'event: stdout\ndata: === Pull ===\n\n',
        'event: stdout\ndata: Fast-forward\n\n',
        'event: done\ndata: {"ok":true,"from":"aaaa1111","to":"bbbb2222","snapshot":"predeploy-shop-1"}\n\n'
      ])
    );
    const events: unknown[] = [];

    await streamDeploy('shop.example', (e) => events.push(e));

    expect(events).toEqual([
      { line: '=== Pull ===' },
      { line: 'Fast-forward' },
      { done: true, ok: true, from: 'aaaa1111', to: 'bbbb2222', snapshot: 'predeploy-shop-1' }
    ]);
  });

  // The pull had landed before the script failed, so the site is on code that
  // was never prepared. Losing that from the done frame is losing the one thing
  // an operator has to act on.
  it('keeps the commits on a failure', async () => {
    apiFetch.mockResolvedValue(
      sse([
        'event: done\ndata: {"ok":false,"error":"the deploy script failed","from":"aaaa1111","to":"bbbb2222"}\n\n'
      ])
    );
    const events: Array<Record<string, unknown>> = [];

    await streamDeploy('shop.example', (e) => events.push(e as Record<string, unknown>));

    expect(events[0]).toMatchObject({
      done: true,
      ok: false,
      error: 'the deploy script failed',
      to: 'bbbb2222'
    });
  });

  // A busy site answers as JSON before the stream starts, so the refusal has to
  // survive a response that is not an event stream at all.
  it('surfaces a refusal that arrives before the stream', async () => {
    apiFetch.mockResolvedValue({
      ok: true,
      headers: { get: () => 'application/json' },
      json: async () => ({ error: 'this site is busy running migrate' })
    } as unknown as Response);
    const events: Array<Record<string, unknown>> = [];

    await streamDeploy('shop.example', (e) => events.push(e as Record<string, unknown>));

    expect(events[0]).toMatchObject({ done: true, ok: false, error: 'this site is busy running migrate' });
  });
});

describe('the exclude list', () => {
  // Saving an empty list and resetting are different requests, because they
  // mean different things: one protects nothing, the other follows the
  // framework again.
  it('sends an empty list as a save and a reset as a reset', async () => {
    apiFetch.mockResolvedValue({ json: async () => ({ ok: true }) } as unknown as Response);

    await saveSiteDeployExclude('shop.example', []);
    expect(JSON.parse(apiFetch.mock.calls[0][1].body)).toEqual({ paths: [] });

    await resetSiteDeployExclude('shop.example');
    expect(JSON.parse(apiFetch.mock.calls[1][1].body)).toEqual({ reset: true });
  });
});
