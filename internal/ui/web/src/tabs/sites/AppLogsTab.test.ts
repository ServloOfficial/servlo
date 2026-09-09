import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import Harness from './AppLogsTab.test.svelte';
import type { Site } from '$stores/sites';

function siteWith(extra: Partial<Site> = {}): Site {
  return {
    name: 'whitewaters',
    domain: 'theregistry.test',
    ...extra
  } as Site;
}

describe('AppLogsTab', () => {
  const realFetch = globalThis.fetch;
  let calls: string[];

  beforeEach(() => {
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

  it('clears logs only after the confirmation modal is confirmed', async () => {
    const methodCalls: string[] = [];
    globalThis.fetch = vi.fn(async (url: string, init?: RequestInit) => {
      methodCalls.push((init?.method || 'GET') + ' ' + url);
      const seg = url.match(/\/api\/app-logs\/[^/?]+(?:\/([^?]+))?/)?.[1];
      if (seg === 'clear') {
        return new Response(JSON.stringify({ ok: true, files_cleared: 1, bytes_cleared: 2048 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        });
      }
      if (seg) {
        return new Response(JSON.stringify({ entries: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        });
      }
      return new Response(JSON.stringify({ files: [{ name: 'laravel.log', size: 2048 }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }) as unknown as typeof fetch;

    render(Harness, { props: { site: siteWith() } });
    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    // Opening the modal must not delete anything on its own.
    const btn = (await screen.findByTitle(/reclaim disk/i)) as HTMLButtonElement;
    btn.click();
    flushSync();
    expect(methodCalls.some((c) => c.includes('/clear'))).toBe(false);

    // The modal's confirm button is what executes the delete.
    const confirm = (await screen.findByRole('button', { name: 'Clear logs' })) as HTMLButtonElement;
    confirm.click();
    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    expect(methodCalls.some((c) => c.startsWith('POST') && c.includes('/clear'))).toBe(true);
  });

  // The level pill is as wide as its word, so a page mixing INFO with WARNING
  // used to step the timestamp and message columns left and right row by row.
  // The cell holding the pill is what keeps them straight.
  it('gives every level the same width so the columns below it line up', async () => {
    globalThis.fetch = vi.fn(async (url: string) => {
      const seg = url.match(/\/api\/app-logs\/[^/?]+(?:\/([^?]+))?/)?.[1];
      if (seg) {
        return new Response(
          JSON.stringify({
            entries: [
              { level: 'INFO', date: '2026-09-09 11:05:35', message: 'Cache warmed' },
              { level: 'EMERGENCY', date: '2026-09-09 11:04:45', message: 'Disk full' },
              { level: undefined, date: '2026-09-09 11:02:55', message: 'Unlabelled' }
            ]
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }
      return new Response(JSON.stringify({ files: [{ name: 'laravel.log', size: 2048 }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }) as unknown as typeof fetch;

    const { container } = render(Harness, { props: { site: siteWith() } });
    await screen.findByText('Cache warmed');

    const cells = [...container.querySelectorAll('[data-level-cell]')];
    expect(cells.length).toBe(3);
    const widths = new Set(cells.map((c) => [...c.classList].find((n) => n.startsWith('w-'))));
    expect(widths.size).toBe(1);
    expect([...widths][0]).toBeTruthy();
  });
});
