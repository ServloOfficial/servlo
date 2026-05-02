import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('sites store', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it('derives php and node site counts', async () => {
    const { sites, sitesByPhp, sitesByNode, phpSiteCount, nodeSiteCount } = await import('./sites');
    sites.set([
      { domain: 'a.test', php_version: '8.4', node_version: '22' },
      { domain: 'b.test', php_version: '8.5', node_version: '22' },
      { domain: 'c.test', php_version: '8.5' }
    ]);
    expect(get(sitesByPhp).get('8.5')).toBe(2);
    expect(get(sitesByPhp).get('8.4')).toBe(1);
    expect(phpSiteCount('8.5')).toBe(2);
    expect(get(sitesByNode).get('22')).toBe(2);
    expect(nodeSiteCount('22')).toBe(2);
    expect(nodeSiteCount('24')).toBe(0);
  });

  it('activeWorktreeDomain returns the parent domain when branch is empty', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = {
      domain: 'acme.test',
      worktrees: [{ branch: 'feat-a', domain: 'feat-a.acme.test' }]
    };
    expect(activeWorktreeDomain(s, '')).toBe('acme.test');
  });

  it('activeWorktreeDomain returns the worktree domain when branch matches', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = {
      domain: 'acme.test',
      worktrees: [{ branch: 'feat-a', domain: 'feat-a.acme.test' }]
    };
    expect(activeWorktreeDomain(s, 'feat-a')).toBe('feat-a.acme.test');
  });

  it('activeWorktreeDomain falls back to the parent when branch is unknown', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = { domain: 'acme.test', worktrees: [] };
    expect(activeWorktreeDomain(s, 'mystery')).toBe('acme.test');
  });
});
