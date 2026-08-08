import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SitePHPSettings } from '$stores/sites';

const settings: SitePHPSettings = {
  max_upload_mb: 64,
  max_execution_seconds: 0,
  memory_limit_mb: 0,
  max_upload_ceiling_mb: 16384,
  max_execution_ceiling_s: 86400,
  memory_limit_ceiling_mb: 65536
};

const loadSitePHPSettings = vi.fn(async () => settings);
const saveSitePHPSettings = vi.fn(async () => ({ ok: true }));

const nginxSettings = {
  static_cache_days: 0,
  response_headers: [],
  static_cache_ceiling_days: 365,
  canonical_host: '',
  canonical_available: true,
  apex_host: 'shop.example',
  www_host: 'www.shop.example',
  redirect_to: '',
  redirect_permanent: false,
  redirects: []
};
const loadSiteNginxSettings = vi.fn(async () => nginxSettings);
const saveSiteNginxSettings = vi.fn(async () => ({ ok: true }));

vi.mock('$stores/sites', () => ({
  loadSitePHPSettings: (...a: unknown[]) => loadSitePHPSettings(...(a as [])),
  saveSitePHPSettings: (...a: unknown[]) => saveSitePHPSettings(...(a as [])),
  loadSiteNginxSettings: (...a: unknown[]) => loadSiteNginxSettings(...(a as [])),
  saveSiteNginxSettings: (...a: unknown[]) => saveSiteNginxSettings(...(a as []))
}));

import SitePHPSettingsTab from './SitePHPSettingsTab.svelte';
import { m } from '../../paraglide/messages.js';

const site = { domain: 'shop.example' } as never;
const props = { site, onOpenRaw: () => {} } as never;

beforeEach(() => {
  loadSitePHPSettings.mockClear();
  saveSitePHPSettings.mockClear();
  loadSiteNginxSettings.mockClear();
  saveSiteNginxSettings.mockClear();
});

describe('SitePHPSettingsTab', () => {
  it('shows the three fields, and no half of a pair on its own', async () => {
    const { findByLabelText, queryByLabelText } = render(SitePHPSettingsTab, { props });

    expect(await findByLabelText(m.sites_phpSettings_upload())).toBeTruthy();
    expect(await findByLabelText(m.sites_phpSettings_execution())).toBeTruthy();
    expect(await findByLabelText(m.sites_phpSettings_memory())).toBeTruthy();
    // The directives the combined fields stand in for are never their own
    // controls: offering post_max_size beside upload_max_filesize is exactly
    // how one gets raised without the other.
    for (const half of ['post_max_size', 'upload_max_filesize', 'client_max_body_size']) {
      expect(queryByLabelText(half)).toBeNull();
    }
  });

  it('sends every field on save, so clearing one is not read as leaving it alone', async () => {
    const { findByLabelText, getByRole } = render(SitePHPSettingsTab, { props });

    const upload = (await findByLabelText(m.sites_phpSettings_upload())) as HTMLInputElement;
    await fireEvent.input(upload, { target: { value: '' } });
    await fireEvent.click(getByRole('button', { name: m.sites_phpSettings_savePhp() }));

    await waitFor(() => {
      expect(saveSitePHPSettings).toHaveBeenCalledWith('shop.example', {
        max_upload_mb: 0,
        max_execution_seconds: 0,
        memory_limit_mb: 0
      });
    });
  });

  it('sends a typed limit as a number rather than the string in the box', async () => {
    const { findByLabelText, getByRole } = render(SitePHPSettingsTab, { props });

    const upload = (await findByLabelText(m.sites_phpSettings_upload())) as HTMLInputElement;
    await fireEvent.input(upload, { target: { value: '256' } });
    await fireEvent.click(getByRole('button', { name: m.sites_phpSettings_savePhp() }));

    await waitFor(() => {
      expect(saveSitePHPSettings).toHaveBeenCalledWith('shop.example', {
        max_upload_mb: 256,
        max_execution_seconds: 0,
        memory_limit_mb: 0
      });
    });
  });

  it('reports a refused save rather than showing it as saved', async () => {
    saveSitePHPSettings.mockResolvedValueOnce({ ok: false, error: 'the max upload size is out of range' } as never);
    const { findByText, getByRole } = render(SitePHPSettingsTab, { props });

    await findByText(m.sites_phpSettings_title());
    await fireEvent.click(getByRole('button', { name: m.sites_phpSettings_savePhp() }));

    expect(await findByText('the max upload size is out of range')).toBeTruthy();
  });
});

describe('the nginx card underneath', () => {
  it('sends only the header rows that were filled in', async () => {
    const { findByText, getByRole, getAllByRole } = render(SitePHPSettingsTab, { props });

    await findByText(m.sites_nginxSettings_title());
    await fireEvent.click(getByRole('button', { name: m.sites_nginxSettings_addHeader() }));
    await fireEvent.click(getByRole('button', { name: m.sites_nginxSettings_addHeader() }));

    const boxes = getAllByRole('textbox') as HTMLInputElement[];
    await fireEvent.input(boxes[0], { target: { value: 'X-Frame-Options' } });
    await fireEvent.input(boxes[1], { target: { value: 'DENY' } });
    // The second row is left blank, which is a row the operator started and
    // abandoned rather than a header called "".
    await fireEvent.click(getByRole('button', { name: m.sites_nginxSettings_saveNginx() }));

    await waitFor(() => {
      expect(saveSiteNginxSettings).toHaveBeenCalledWith('shop.example', {
        static_cache_days: 0,
        response_headers: [{ name: 'X-Frame-Options', value: 'DENY' }],
        canonical_host: '',
        redirect_to: '',
        redirect_permanent: false,
        redirects: []
      });
    });
  });

  it('offers the raw editor underneath the fields', async () => {
    let opened = false;
    const { findByRole } = render(SitePHPSettingsTab, {
      props: { site, onOpenRaw: () => (opened = true) } as never
    });

    await fireEvent.click(await findByRole('button', { name: m.sites_nginxSettings_rawOpen() }));
    expect(opened).toBe(true);
  });
});

describe('the canonical domain toggle', () => {
  it('offers both hosts by name and sends the choice', async () => {
    const { findByLabelText, getByRole } = render(SitePHPSettingsTab, { props });

    const select = (await findByLabelText(m.sites_nginxSettings_canonical())) as HTMLSelectElement;
    // Named, not "www" and "non-www": the operator is picking between two
    // addresses their visitors will see, so the addresses are what is shown.
    expect([...select.options].map((o) => o.value)).toEqual(['', 'apex', 'www']);
    expect([...select.options].map((o) => o.textContent?.trim())).toContain('www.shop.example');

    await fireEvent.change(select, { target: { value: 'apex' } });
    await fireEvent.click(getByRole('button', { name: m.sites_nginxSettings_saveNginx() }));

    await waitFor(() => {
      expect(saveSiteNginxSettings).toHaveBeenCalledWith('shop.example', {
        static_cache_days: 0,
        response_headers: [],
        canonical_host: 'apex',
        redirect_to: '',
        redirect_permanent: false,
        redirects: []
      });
    });
  });

  // A site with one domain has nothing to redirect to. Offering the choice
  // would mean offering a save the server refuses.
  it('explains itself instead of offering a choice the site cannot make', async () => {
    loadSiteNginxSettings.mockResolvedValueOnce({
      ...nginxSettings,
      canonical_available: false
    } as never);
    const { findByText, queryByLabelText } = render(SitePHPSettingsTab, { props });

    expect(await findByText(m.sites_nginxSettings_canonicalUnavailable())).toBeTruthy();
    expect(queryByLabelText(m.sites_nginxSettings_canonical())).toBeNull();
  });
});

describe('the redirects card', () => {
  it('sends a whole-domain redirect and its permanence', async () => {
    const { findByLabelText, getByRole, getByLabelText } = render(SitePHPSettingsTab, { props });

    const whole = (await findByLabelText(m.sites_redirects_whole())) as HTMLInputElement;
    await fireEvent.input(whole, { target: { value: ' https://newshop.example ' } });
    await fireEvent.click(getByLabelText(m.sites_redirects_permanent()));
    await fireEvent.click(getByRole('button', { name: m.sites_redirects_save() }));

    await waitFor(() => {
      expect(saveSiteNginxSettings).toHaveBeenCalledWith(
        'shop.example',
        expect.objectContaining({
          // Trimmed: a pasted URL with a stray space is not a different URL,
          // and the server would refuse it as one.
          redirect_to: 'https://newshop.example',
          redirect_permanent: true
        })
      );
    });
  });

  it('sends only the rules that were filled in', async () => {
    const { findByRole, getByRole, getByLabelText } = render(SitePHPSettingsTab, { props });

    const add = await findByRole('button', { name: m.sites_redirects_add() });
    await fireEvent.click(add);
    await fireEvent.click(add);

    await fireEvent.input(getByLabelText(`${m.sites_redirects_from()} 1`), {
      target: { value: '/old' }
    });
    await fireEvent.input(getByLabelText(`${m.sites_redirects_to()} 1`), {
      target: { value: '/new' }
    });
    await fireEvent.click(getByRole('button', { name: m.sites_redirects_save() }));

    await waitFor(() => {
      expect(saveSiteNginxSettings).toHaveBeenCalledWith(
        'shop.example',
        expect.objectContaining({
          redirects: [{ from: '/old', to: '/new', permanent: false }]
        })
      );
    });
  });

  it('reports a refused redirect rather than showing it as saved', async () => {
    saveSiteNginxSettings.mockResolvedValueOnce({
      ok: false,
      error: 'this site answers for shop.example, so redirecting the whole domain there is a loop'
    } as never);
    const { findByRole, findByText } = render(SitePHPSettingsTab, { props });

    await fireEvent.click(await findByRole('button', { name: m.sites_redirects_save() }));

    expect(
      await findByText(
        'this site answers for shop.example, so redirecting the whole domain there is a loop'
      )
    ).toBeTruthy();
  });

// The two cards send one request, so a failure has two places it could appear
// and appearing in both reads as two separate problems.
  it('shows a save failure once, beside the button that was pressed', async () => {
  saveSiteNginxSettings.mockResolvedValueOnce({ ok: false, error: 'nginx said no' } as never);
  const { findByRole, findAllByText } = render(SitePHPSettingsTab, { props });

  await fireEvent.click(await findByRole('button', { name: m.sites_redirects_save() }));

  expect(await findAllByText('nginx said no')).toHaveLength(1);
  });
});
