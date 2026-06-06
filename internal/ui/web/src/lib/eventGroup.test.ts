import { describe, it, expect } from 'vitest';
import { groupKey, sitePrefix } from './eventGroup';
import type { DumpEvent } from '$lib/dumpsStream';

function ev(over: Partial<DumpEvent['ctx']> & { ts?: string } = {}): DumpEvent {
  const { ts, ...ctx } = over;
  return {
    v: 1,
    id: 'x',
    ts: ts ?? '2026-05-10T12:00:00.000Z',
    kind: 'query',
    ctx: { type: 'fpm', site: 'acme', request: 'GET /checkout', pid: 7, ...ctx },
    src: { file: '/x.php', line: 1 }
  };
}

describe('groupKey', () => {
  it('separates a worktree request from the parent it shares site/request/pid with', () => {
    const parent = groupKey(ev({ branch: '' }));
    const worktree = groupKey(ev({ branch: 'feature-x' }));
    expect(parent).not.toBe(worktree);
  });

  it('keeps the per-request id as the boundary when present', () => {
    expect(groupKey(ev({ rid: 'r1', branch: 'feature-x' }))).toBe('rid:r1');
  });

  it('ignores rid for dump events (it can be volatile without the extension)', () => {
    // Two dumps in one request whose rids differ (collector new_id() fallback)
    // must still land in the same group, so dumps key on request, not rid.
    const a = groupKey({ ...ev({ rid: 'vol-1' }), kind: 'dump' });
    const b = groupKey({ ...ev({ rid: 'vol-2' }), kind: 'dump' });
    expect(a).toBe(b);
    expect(a.startsWith('rid:')).toBe(false);
  });

  it('folds branch into the cli bucket key too', () => {
    const parent = groupKey(ev({ type: 'cli', branch: '' }));
    const worktree = groupKey(ev({ type: 'cli', branch: 'feature-x' }));
    expect(parent).not.toBe(worktree);
  });
});

describe('sitePrefix', () => {
  it('tags the branch alongside the site', () => {
    expect(sitePrefix(ev({ branch: 'feature-x' }), false)).toBe('[acme@feature-x] ');
  });

  it('is plain [site] with no branch', () => {
    expect(sitePrefix(ev({ branch: '' }), false)).toBe('[acme] ');
  });

  it('keeps the branch but drops the site when the site prefix is hidden', () => {
    expect(sitePrefix(ev({ branch: 'feature-x' }), true)).toBe('[feature-x] ');
    expect(sitePrefix(ev({ branch: '' }), true)).toBe('');
  });
});
