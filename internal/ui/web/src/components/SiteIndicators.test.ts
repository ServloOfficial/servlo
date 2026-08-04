import { render } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import SiteIndicators from './SiteIndicators.svelte';
import type { Site } from '$stores/sites';

function site(overrides: Partial<Site>): Site {
  return { domain: 'app.test', name: 'app', has_queue_worker: true, ...overrides } as Site;
}

describe('SiteIndicators', () => {
  it('flags a failing worker with its own marker', () => {
    const { getByTitle } = render(SiteIndicators, {
      props: { site: site({ queue_running: false, queue_failing: true }) }
    });
    expect(getByTitle('Worker failing')).toBeInTheDocument();
  });

  it('shows no failure marker while every worker is healthy', () => {
    const { queryByTitle } = render(SiteIndicators, {
      props: { site: site({ queue_running: true }) }
    });
    expect(queryByTitle('Worker failing')).toBeNull();
  });

  it('marks a site that has git worktrees', () => {
    const { getByTitle } = render(SiteIndicators, {
      props: { site: site({ worktrees: [{ branch: 'feat' }] as Site['worktrees'] }) }
    });
    expect(getByTitle('Git Worktrees')).toBeInTheDocument();
  });
});
