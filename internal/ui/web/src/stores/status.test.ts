import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('status store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loads status from /api/status', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          nginx: { running: true },
          php_fpms: [{ version: '8.5', running: true }],
          php_default: '8.5',
          node_default: '22',
          node_managed_by_servlo: true,
          watcher_running: true
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    ) as unknown as typeof fetch;

    const { status, loadStatus, statusLoaded } = await import('./status');
    await loadStatus();
    expect(get(statusLoaded)).toBe(true);
    expect(get(status).php_default).toBe('8.5');
  });

  it('servloStatusColor is gray before load', async () => {
    const { servloStatusColor } = await import('./status');
    expect(get(servloStatusColor)).toBe('gray');
  });

  it('servloStatusColor is red when core is broken', async () => {
    const { status, statusLoaded, servloStatusColor } = await import('./status');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      nginx: { running: false },
      watcher_running: true
    }));
    expect(get(servloStatusColor)).toBe('red');
  });

  it('servloStatusColor is yellow when healthy with update', async () => {
    const { status, statusLoaded, servloStatusColor } = await import('./status');
    const { version } = await import('./version');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      nginx: { running: true },
      watcher_running: true
    }));
    version.update((v) => ({ ...v, hasUpdate: true }));
    expect(get(servloStatusColor)).toBe('yellow');
  });

  it('servloStatusColor is green when healthy', async () => {
    const { status, statusLoaded, servloStatusColor } = await import('./status');
    const { version } = await import('./version');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      nginx: { running: true },
      watcher_running: true
    }));
    version.update((v) => ({ ...v, hasUpdate: false }));
    expect(get(servloStatusColor)).toBe('green');
  });

  // The System health card lists every PHP-FPM pool with its own dot, and the
  // System tab offers to start everything when one is down. The card's summary
  // pill used to look only at nginx and the watcher, so a dead pool showed a red
  // dot with the word Healthy above it, on the page an operator lands on.
  it('servloStatusColor is red when a php-fpm pool the card lists is down', async () => {
    const { status, statusLoaded, servloStatusColor } = await import('./status');
    const { version } = await import('./version');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      nginx: { running: true },
      watcher_running: true,
      php_fpms: [
        { version: '8.4', running: true },
        { version: '8.5', running: false }
      ]
    }));
    version.update((v) => ({ ...v, hasUpdate: false }));
    expect(get(servloStatusColor)).toBe('red');
  });
});

describe('server restart reload', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it('reloads once the server reports a different instance', async () => {
    const reload = vi.fn();
    const { applyStatus } = await import('./status');

    applyStatus({ instance: 'first' }, reload);
    expect(reload).not.toHaveBeenCalled();

    applyStatus({ instance: 'first' }, reload);
    expect(reload).not.toHaveBeenCalled();

    applyStatus({ instance: 'second' }, reload);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('posts one tool update and carries the refusal back to the caller', async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ ok: false, error: 'checksum mismatch' }))
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const { updateTool } = await import('./status');

    const res = await updateTool('composer');

    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/tools/composer/update');
    expect(fetchMock.mock.calls[0][1]?.method).toBe('POST');
    expect(res).toEqual({ ok: false, error: 'checksum mismatch' });
  });

  // http.Error answers in plain text, so the envelope decode has to cope with a
  // body that is not JSON rather than surface a parse error as the tool's fault.
  it('reads a plain-text refusal as an error', async () => {
    globalThis.fetch = vi.fn(async () => new Response('method not allowed', { status: 405 })) as unknown as typeof fetch;
    const { updateTool } = await import('./status');

    expect(await updateTool('composer')).toEqual({ ok: false, error: 'method not allowed' });
  });

  it('ignores a payload with no instance, so an older server never loops', async () => {
    const reload = vi.fn();
    const { applyStatus } = await import('./status');

    applyStatus({ instance: 'first' }, reload);
    applyStatus({}, reload);
    expect(reload).not.toHaveBeenCalled();
  });
});
