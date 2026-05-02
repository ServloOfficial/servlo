import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('appLogs store', () => {
  const realFetch = globalThis.fetch;
  let calls: string[];

  beforeEach(() => {
    vi.resetModules();
    calls = [];
    globalThis.fetch = vi.fn(async (url: string) => {
      calls.push(url);
      return new Response(JSON.stringify({ files: [], entries: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }) as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('listAppLogFiles omits ?branch when not provided', async () => {
    const { listAppLogFiles } = await import('./appLogs');
    await listAppLogFiles('acme.test');
    expect(calls).toHaveLength(1);
    expect(calls[0]).toContain('/api/app-logs/acme.test');
    expect(calls[0]).not.toContain('branch=');
  });

  it('listAppLogFiles includes ?branch= when provided', async () => {
    const { listAppLogFiles } = await import('./appLogs');
    await listAppLogFiles('acme.test', 'feat-a');
    expect(calls[0]).toMatch(/[?&]branch=feat-a(&|$)/);
  });

  it('loadAppLogEntries omits branch when blank', async () => {
    const { loadAppLogEntries } = await import('./appLogs');
    await loadAppLogEntries('acme.test', 'laravel.log', false);
    expect(calls[0]).toContain('limit=100');
    expect(calls[0]).not.toContain('branch=');
  });

  it('loadAppLogEntries includes branch alongside limit', async () => {
    const { loadAppLogEntries } = await import('./appLogs');
    await loadAppLogEntries('acme.test', 'laravel.log', true, 'feat-a');
    expect(calls[0]).toContain('limit=0');
    expect(calls[0]).toMatch(/[?&]branch=feat-a(&|$)/);
  });

  it('loadAppLogEntries url-encodes filenames containing dots', async () => {
    const { loadAppLogEntries } = await import('./appLogs');
    await loadAppLogEntries('acme.test', 'laravel-2026-05-02.log', false, 'feat-a');
    expect(calls[0]).toContain('laravel-2026-05-02.log');
  });
});
