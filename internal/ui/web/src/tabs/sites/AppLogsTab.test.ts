import { render } from '@testing-library/svelte';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import Harness from './AppLogsTab.test.svelte';
import type { Site } from '$stores/sites';

function siteWith(extra: Partial<Site> = {}): Site {
  return {
    name: 'whitewaters',
    domain: 'theregistry.test',
    branch: 'main',
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

  it('re-fetches the file list when branch prop changes', async () => {
    const { rerender } = render(Harness, {
      props: { site: siteWith(), branch: '' }
    });

    // Allow the initial $effect to fire and the awaited fetch to resolve.
    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    const parentCalls = calls.filter((u) => u.startsWith('/api/app-logs/theregistry.test'));
    expect(parentCalls.length).toBeGreaterThan(0);
    expect(parentCalls.some((u) => u.includes('branch='))).toBe(false);

    // Now switch to a worktree branch. The effect must re-fire so the
    // dropdown is scoped to the worktree path, not the parent's.
    calls.length = 0;
    await rerender({ site: siteWith(), branch: 'main' });
    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    const wtCalls = calls.filter((u) => u.startsWith('/api/app-logs/theregistry.test'));
    expect(wtCalls.length).toBeGreaterThan(0);
    expect(wtCalls.some((u) => /[?&]branch=main(&|$)/.test(u))).toBe(true);
  });

  it('re-fetches when switching back from worktree to parent', async () => {
    const { rerender } = render(Harness, {
      props: { site: siteWith(), branch: 'feat-x' }
    });

    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    calls.length = 0;
    await rerender({ site: siteWith(), branch: '' });
    await Promise.resolve();
    await Promise.resolve();
    flushSync();
    await Promise.resolve();

    const parentCalls = calls.filter((u) => u.startsWith('/api/app-logs/theregistry.test'));
    expect(parentCalls.length).toBeGreaterThan(0);
    expect(parentCalls.every((u) => !u.includes('branch='))).toBe(true);
  });
});
