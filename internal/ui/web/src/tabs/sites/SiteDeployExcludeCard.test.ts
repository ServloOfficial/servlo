import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SiteDeployExclude } from '$stores/deploy';

let current: SiteDeployExclude = {
  paths: ['wp-content/uploads', 'wp-content/plugins'],
  custom: false,
  default: ['wp-content/uploads', 'wp-content/plugins']
};

const loadSiteDeployExclude = vi.fn(async () => current);
const saveSiteDeployExclude = vi.fn(async () => ({ ok: true }));
const resetSiteDeployExclude = vi.fn(async () => ({ ok: true }));

vi.mock('$stores/deploy', () => ({
  loadSiteDeployExclude: (...a: unknown[]) => loadSiteDeployExclude(...(a as [])),
  saveSiteDeployExclude: (...a: unknown[]) => saveSiteDeployExclude(...(a as [])),
  resetSiteDeployExclude: (...a: unknown[]) => resetSiteDeployExclude(...(a as []))
}));

import SiteDeployExcludeCard from './SiteDeployExcludeCard.svelte';
import { m } from '../../paraglide/messages.js';

const props = { site: { domain: 'shop.example' } } as never;

beforeEach(() => {
  current = {
    paths: ['wp-content/uploads', 'wp-content/plugins'],
    custom: false,
    default: ['wp-content/uploads', 'wp-content/plugins']
  };
  loadSiteDeployExclude.mockClear();
  saveSiteDeployExclude.mockClear();
  resetSiteDeployExclude.mockClear();
});

describe('SiteDeployExcludeCard', () => {
  it('shows the framework list and says where it came from', async () => {
    const { findByText, container } = render(SiteDeployExcludeCard, { props });

    await findByText(m.sites_deployExclude_sourceFramework());
    const box = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(box.value).toBe('wp-content/uploads\nwp-content/plugins');
  });

  // The reset offer only makes sense once the site has a list of its own to
  // reset, and it names what going back would restore.
  it('offers the reset only when the site has its own list', async () => {
    const { queryByText, findByText } = render(SiteDeployExcludeCard, { props });
    await findByText(m.sites_deployExclude_sourceFramework());
    expect(
      queryByText(m.sites_deployExclude_reset({ paths: 'wp-content/uploads, wp-content/plugins' }))
    ).toBeNull();

    current = { paths: ['storage/app'], custom: true, default: ['wp-content/uploads'] };
    const own = render(SiteDeployExcludeCard, { props });
    await own.findByText(m.sites_deployExclude_sourceSite());
    await own.findByText(m.sites_deployExclude_reset({ paths: 'wp-content/uploads' }));
  });

  it('saves one path per line, dropping the blanks a text box leaves behind', async () => {
    const { findByText, container } = render(SiteDeployExcludeCard, { props });
    await findByText(m.sites_deployExclude_sourceFramework());

    const box = container.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(box, { target: { value: '  storage/app  \n\n   \nvar/media\n' } });
    await fireEvent.click(await findByText(m.sites_deployExclude_save()));

    await waitFor(() =>
      expect(saveSiteDeployExclude).toHaveBeenCalledWith('shop.example', [
        'storage/app',
        'var/media'
      ])
    );
  });

  // Clearing the box is a real choice, not a mistake, so it says what it means
  // rather than silently doing nothing or quietly restoring the framework's.
  it('warns when the list is empty', async () => {
    const { findByText, container } = render(SiteDeployExcludeCard, { props });
    await findByText(m.sites_deployExclude_sourceFramework());

    const box = container.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(box, { target: { value: '' } });

    await findByText(m.sites_deployExclude_emptyWarning());
  });

  it('resets through the reset call, not by saving an empty list', async () => {
    current = { paths: ['storage/app'], custom: true, default: ['wp-content/uploads'] };
    const { findByText } = render(SiteDeployExcludeCard, { props });

    await fireEvent.click(
      await findByText(m.sites_deployExclude_reset({ paths: 'wp-content/uploads' }))
    );

    await waitFor(() => expect(resetSiteDeployExclude).toHaveBeenCalledWith('shop.example'));
    expect(saveSiteDeployExclude).not.toHaveBeenCalled();
  });

  it('shows the refusal rather than reporting a save that did not happen', async () => {
    saveSiteDeployExclude.mockResolvedValueOnce({
      ok: false,
      error: 'path "../../etc" points outside the site directory'
    } as never);
    const { findByText } = render(SiteDeployExcludeCard, { props });
    await findByText(m.sites_deployExclude_sourceFramework());

    await fireEvent.click(await findByText(m.sites_deployExclude_save()));

    await findByText('path "../../etc" points outside the site directory');
  });
  // Every framework but WordPress protects nothing, so this is what the card
  // looks like on most sites: it used to say it was following a list that did
  // not exist, directly above the warning saying nothing was protected.
  it('says nothing about a framework list that is empty', async () => {
    current = { paths: [], custom: false, default: [] };
    const { findByText, queryByText } = render(SiteDeployExcludeCard, { props });

    await findByText(m.sites_deployExclude_emptyWarning());
    expect(queryByText(m.sites_deployExclude_sourceFramework())).toBeNull();
  });

  // A site that emptied its own list chose that, so the warning still says whose
  // list it is.
  it('still names the site when the site emptied its own list', async () => {
    current = { paths: [], custom: true, default: ['wp-content/uploads'] };
    const { findByText } = render(SiteDeployExcludeCard, { props });

    await findByText(m.sites_deployExclude_sourceSite());
    await findByText(m.sites_deployExclude_emptyWarning());
  });
});
