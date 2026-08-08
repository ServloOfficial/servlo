import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SiteWebhook } from '$stores/deploy';

const off: SiteWebhook = {
  enabled: false,
  signature_header: 'X-Hub-Signature-256',
  delivery_header: 'X-GitHub-Delivery'
};
const on: SiteWebhook = {
  enabled: true,
  url: '/api/webhooks/deploy/abc123',
  branch: 'main',
  signature_header: 'X-Hub-Signature-256',
  delivery_header: 'X-GitHub-Delivery'
};

let current: SiteWebhook = off;
const loadSiteWebhook = vi.fn(async () => current);
const saveSiteWebhook = vi.fn(async (_d: string, v: Record<string, unknown>) => {
  if (!v.enabled) return off;
  return { ...on, branch: String(v.branch ?? ''), secret: v.regenerate ? 'new-secret' : 'minted-secret' };
});

vi.mock('$stores/deploy', () => ({
  loadSiteWebhook: (...a: unknown[]) => loadSiteWebhook(...(a as [])),
  saveSiteWebhook: (...a: unknown[]) => saveSiteWebhook(...(a as [never, never]))
}));

import SiteWebhookCard from './SiteWebhookCard.svelte';
import { m } from '../../paraglide/messages.js';

const props = { site: { domain: 'shop.example' } } as never;

beforeEach(() => {
  current = off;
  loadSiteWebhook.mockClear();
  saveSiteWebhook.mockClear();
});

describe('SiteWebhookCard', () => {
  // Off until somebody turns it on, and the card says so rather than showing an
  // endpoint that does not answer.
  it('starts off and offers nothing to copy', async () => {
    const { findByText, queryByText } = render(SiteWebhookCard, { props });

    await findByText(m.sites_webhook_off());
    await findByText(m.sites_webhook_enable());
    expect(queryByText(m.sites_webhook_url())).toBeNull();
  });

  // The secret is shown exactly once, when it is minted, because that is the
  // only moment it can be copied into the repository.
  it('shows the secret when it is minted, with the warning', async () => {
    const { findByText } = render(SiteWebhookCard, { props });

    await fireEvent.click(await findByText(m.sites_webhook_enable()));

    await findByText('minted-secret');
    await findByText(m.sites_webhook_secretOnce());
  });

  // And a site that already had one shows no secret at all, because the server
  // does not send it back.
  it('shows no secret for a webhook that already exists', async () => {
    current = on;
    const { findByText, queryByText } = render(SiteWebhookCard, { props });

    await findByText(m.sites_webhook_secretHidden());
    expect(queryByText('minted-secret')).toBeNull();
  });

  // A path is not something anyone can paste into GitHub.
  it('shows the full URL, not the path', async () => {
    current = on;
    const { findByText } = render(SiteWebhookCard, { props });

    await findByText(new URL('/api/webhooks/deploy/abc123', location.origin).href);
  });

  it('saves the branch filter', async () => {
    current = on;
    const { findByText, findByLabelText } = render(SiteWebhookCard, { props });

    const input = await findByLabelText(m.sites_webhook_branch());
    await fireEvent.input(input, { target: { value: '  release  ' } });
    await fireEvent.click(await findByText(m.sites_webhook_save()));

    await waitFor(() =>
      expect(saveSiteWebhook).toHaveBeenCalledWith('shop.example', {
        branch: 'release',
        enabled: true
      })
    );
  });

  // Regenerating is a different request from saving, and it produces a secret
  // to copy because the old one has just stopped working.
  it('regenerates the secret and shows the new one', async () => {
    current = on;
    const { findByText } = render(SiteWebhookCard, { props });

    await fireEvent.click(await findByText(m.sites_webhook_regenerate()));

    await waitFor(() =>
      expect(saveSiteWebhook).toHaveBeenCalledWith('shop.example', {
        branch: 'main',
        enabled: true,
        regenerate: true
      })
    );
    await findByText('new-secret');
  });

  // Turning it off goes back to the off state and stops showing a secret that
  // no longer authorises anything.
  it('turns off and forgets the secret on screen', async () => {
    current = on;
    const { findByText, queryByText } = render(SiteWebhookCard, { props });

    await fireEvent.click(await findByText(m.sites_webhook_regenerate()));
    await findByText('new-secret');

    await fireEvent.click(await findByText(m.sites_webhook_disable()));

    await findByText(m.sites_webhook_off());
    expect(queryByText('new-secret')).toBeNull();
  });

  it('shows the server refusal rather than reporting a save', async () => {
    current = on;
    saveSiteWebhook.mockResolvedValueOnce({ ...on, error: 'this site has no webhook to regenerate' } as never);
    const { findByText } = render(SiteWebhookCard, { props });

    await fireEvent.click(await findByText(m.sites_webhook_save()));

    await findByText('this site has no webhook to regenerate');
  });
});
