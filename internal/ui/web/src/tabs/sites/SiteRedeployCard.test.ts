import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SiteRedeploy } from '$stores/deploy';

let current: SiteRedeploy = { available: true, commit: 'aaaa1111bbbb' };

const loadSiteRedeploy = vi.fn(async () => current);
const streamRedeploy = vi.fn(async (_d: string, cb: (e: unknown) => void) => {
  cb({ line: '=== Going back to aaaa1111 ===' });
  cb({ done: true, ok: true, from: 'cccc3333', to: 'aaaa1111bbbb' });
});

vi.mock('$stores/deploy', () => ({
  loadSiteRedeploy: (...a: unknown[]) => loadSiteRedeploy(...(a as [])),
  streamRedeploy: (...a: unknown[]) => streamRedeploy(...(a as [never, never]))
}));

import SiteRedeployCard from './SiteRedeployCard.svelte';
import { m } from '../../paraglide/messages.js';

const props = { site: { domain: 'shop.example' } } as never;

beforeEach(() => {
  current = { available: true, commit: 'aaaa1111bbbb' };
  loadSiteRedeploy.mockClear();
  streamRedeploy.mockClear();
});

describe('SiteRedeployCard', () => {
  // The operator is about to assume the database goes back too. It does not,
  // and the card has to say so before the button, not after.
  it('states that migrations are not undone', async () => {
    const { findByText } = render(SiteRedeployCard, { props });

    await findByText(m.sites_redeploy_migrationWarning());
    expect(m.sites_redeploy_migrationWarning().toLowerCase()).toContain(
      'migrations are not undone'
    );
  });

  it('names the commit it would go back to', async () => {
    const { findByText } = render(SiteRedeployCard, { props });

    await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' }));
  });

  // One press arms it, the second does it. Going back a deploy changes what
  // the site serves and cannot be undone by pressing it again.
  it('takes two presses', async () => {
    const { findByText } = render(SiteRedeployCard, { props });

    await fireEvent.click(await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' })));
    expect(streamRedeploy).not.toHaveBeenCalled();

    await fireEvent.click(await findByText(m.sites_redeploy_confirm({ commit: 'aaaa1111' })));
    await waitFor(() => expect(streamRedeploy).toHaveBeenCalled());
  });

  it('can be backed out of before the second press', async () => {
    const { findByText } = render(SiteRedeployCard, { props });

    await fireEvent.click(await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' })));
    await fireEvent.click(await findByText(m.sites_redeploy_cancel()));
    await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' }));

    expect(streamRedeploy).not.toHaveBeenCalled();
  });

  // A site before its first deploy has nowhere to go, and says that rather
  // than offering a button that would be refused.
  it('offers nothing when there is no deploy to go back from', async () => {
    current = { available: false };
    const { findByText, queryByText } = render(SiteRedeployCard, { props });

    await findByText(m.sites_redeploy_none());
    expect(queryByText(m.sites_redeploy_action({ commit: '' }))).toBeNull();
  });

  it('streams the output and reports where it landed', async () => {
    const { findByText, container } = render(SiteRedeployCard, { props });

    await fireEvent.click(await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' })));
    await fireEvent.click(await findByText(m.sites_redeploy_confirm({ commit: 'aaaa1111' })));

    await waitFor(() =>
      expect(container.querySelector('pre')?.textContent).toContain('Going back to')
    );
    await findByText(m.sites_redeploy_ok({ commit: 'aaaa1111' }));
  });

  it('shows a refusal rather than claiming it went back', async () => {
    streamRedeploy.mockImplementationOnce(async (_d: string, cb: (e: unknown) => void) => {
      cb({ done: true, ok: false, error: 'this site is busy running deploy' });
    });
    const { findByText } = render(SiteRedeployCard, { props });

    await fireEvent.click(await findByText(m.sites_redeploy_action({ commit: 'aaaa1111' })));
    await fireEvent.click(await findByText(m.sites_redeploy_confirm({ commit: 'aaaa1111' })));

    await findByText('this site is busy running deploy');
  });
});
