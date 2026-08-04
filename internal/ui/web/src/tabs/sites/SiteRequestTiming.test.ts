import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import type { Analytics } from '$stores/analytics';

// Two requests to the same URI in the same millisecond are real (Reverb/websocket
// bursts) and the durable store has no unique constraint, so the Recent list can
// receive rows that share (at_millis, uri). Its {#each} key must stay unique or
// Svelte 5 throws a duplicate-key error at render and blanks the tab.
const analytics: Analytics = {
  site: 'whitewaters',
  range: '1h',
  samples: 2,
  cold_starts: 0,
  median_millis: 5,
  p95_millis: 9,
  status: { c2xx: 2, c3xx: 0, c4xx: 0, c5xx: 0 },
  distribution: [],
  throughput: [],
  routes: [
    {
      route: 'GET /reports/:id',
      method: 'GET',
      example: '/reports/7',
      p50_millis: 40,
      p95_millis: 500,
      recent_p95_millis: 500,
      multiplier: 10,
      samples: 10
    }
  ],
  recent: [
    { at_millis: 1783501663287, method: 'POST', route: 'POST /broadcasting/auth', uri: '/broadcasting/auth', status: 200, millis: 4, cold: false },
    { at_millis: 1783501663287, method: 'POST', route: 'POST /broadcasting/auth', uri: '/broadcasting/auth', status: 200, millis: 6, cold: false }
  ]
};

const loadSiteAnalytics = vi.fn(async () => analytics);

vi.mock('$stores/analytics', () => ({
  loadSiteAnalytics: (...args: unknown[]) => loadSiteAnalytics(...(args as [])),
  TIME_RANGES: ['15m', '1h', '24h', '7d']
}));

import SiteRequestTiming from './SiteRequestTiming.svelte';
import { m } from '../../paraglide/messages.js';

describe('SiteRequestTiming Recent list', () => {
  it('renders same-millisecond, same-URI rows without a duplicate-key crash', async () => {
    const { getByRole, findByText, getAllByText } = render(SiteRequestTiming, {
      props: { site: { domain: 'whitewaters' } }
    });

    // Wait for the loaded view, then switch to the Recent tab.
    await findByText(m.sites_timing_recent());
    await fireEvent.click(getByRole('button', { name: m.sites_timing_recent() }));

    // Both colliding rows render; without a unique key the keyed each throws.
    await waitFor(() => {
      expect(getAllByText('/broadcasting/auth').length).toBe(2);
    });
  });
});

// A worktree is served from its own subdomain, so the panel must both ask for the
// branch's timing. It used to load the parent's, showing the wrong checkout.
describe('SiteRequestTiming on a worktree', () => {
  const site = {
    domain: 'whitewaters.test',
    tls: true,
    worktrees: [{ branch: 'feature-x', domain: 'feature-x.whitewaters.test' }]
  };

  it('loads the branch rather than the parent checkout', async () => {
    loadSiteAnalytics.mockClear();

    const { findAllByText } = render(SiteRequestTiming, {
      props: {
        site,
        activeWorktreeBranch: 'feature-x'
      }
    });

    await waitFor(() => {
      expect(loadSiteAnalytics).toHaveBeenCalledWith('whitewaters.test', '1h', 'feature-x');
    });

    await findAllByText('/reports/:id');
  });
});
