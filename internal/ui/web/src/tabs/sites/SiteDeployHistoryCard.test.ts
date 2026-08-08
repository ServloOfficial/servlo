import { render, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { DeployHistoryEntry } from '$stores/deploy';

let entries: DeployHistoryEntry[] = [];
const loadSiteDeployHistory = vi.fn(async () => entries);

vi.mock('$stores/deploy', () => ({
  loadSiteDeployHistory: (...a: unknown[]) => loadSiteDeployHistory(...(a as []))
}));

import SiteDeployHistoryCard from './SiteDeployHistoryCard.svelte';
import { m } from '../../paraglide/messages.js';

const props = { site: { domain: 'shop.example' } } as never;

beforeEach(() => {
  entries = [];
  loadSiteDeployHistory.mockClear();
});

describe('SiteDeployHistoryCard', () => {
  it('says nothing has deployed yet rather than showing an empty list', async () => {
    const { findByText } = render(SiteDeployHistoryCard, { props });

    await findByText(m.sites_deployHistory_empty());
  });

  // The five things the story asks for: the commit, who wrote it, how long it
  // took, whether it worked, and who set it off.
  it('shows the commit, author, duration, outcome and who triggered it', async () => {
    entries = [
      {
        at: '2026-08-08T12:00:00Z',
        to: 'bbbb2222cccc',
        ok: true,
        author: 'Sam Rivera',
        subject: 'add the orders index',
        actor: 'alice',
        duration_ms: 21480
      }
    ];
    const { findByText } = render(SiteDeployHistoryCard, { props });

    await findByText('bbbb2222');
    await findByText('add the orders index');
    await findByText(m.sites_deployHistory_by({ author: 'Sam Rivera' }));
    await findByText(m.sites_deployHistory_triggeredBy({ actor: 'alice' }));
    await findByText('21.5s');
    await findByText(m.sites_deployHistory_ok());
  });

  // A failure is the entry anyone actually comes looking for, so it carries its
  // reason rather than only a red word.
  it('shows a failure with its reason', async () => {
    entries = [
      {
        at: '2026-08-08T12:00:00Z',
        to: 'cccc3333',
        ok: false,
        error: 'the deploy script failed, and the new code was not made live'
      }
    ];
    const { findByText } = render(SiteDeployHistoryCard, { props });

    await findByText(m.sites_deployHistory_failed());
    await findByText('the deploy script failed, and the new code was not made live');
  });

  // Going back is not the same event as going forward, and a history that
  // rendered them identically would read as if the site kept deploying.
  it('distinguishes a redeploy from a deploy', async () => {
    entries = [{ at: '2026-08-08T12:00:00Z', to: 'aaaa1111', ok: true, redeploy: true }];
    const { findByText, queryByText } = render(SiteDeployHistoryCard, { props });

    await findByText(m.sites_deployHistory_wentBack());
    expect(queryByText(m.sites_deployHistory_ok())).toBeNull();
  });

  // A deploy with no signed-in user says so rather than leaving a blank where a
  // name goes, which would read as a missing field instead of a fact.
  it('says when no signed-in user triggered it', async () => {
    entries = [{ at: '2026-08-08T12:00:00Z', to: 'aaaa1111', ok: true }];
    const { findByText } = render(SiteDeployHistoryCard, { props });

    await findByText(m.sites_deployHistory_automatic());
  });

  // The list is stale the moment a deploy runs above it, so the tab bumps a
  // counter and the card reloads.
  it('reloads when the tab says a deploy finished', async () => {
    const { rerender } = render(SiteDeployHistoryCard, { props: { ...(props as object), reload: 0 } as never });
    await waitFor(() => expect(loadSiteDeployHistory).toHaveBeenCalledTimes(1));

    await rerender({ ...(props as object), reload: 1 } as never);

    await waitFor(() => expect(loadSiteDeployHistory).toHaveBeenCalledTimes(2));
  });
});
