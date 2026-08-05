import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';

const tlsStatus = vi.fn();
const toggleTLS = vi.fn();
const loadSites = vi.fn();
const openErrorModal = vi.fn();

vi.mock('$stores/sites', () => ({ tlsStatus, toggleTLS, loadSites }));
vi.mock('$stores/modals', () => ({ openErrorModal }));

const unsecured = { domain: 'example.com', tls: false } as never;

async function mount(site = unsecured, toggleable = true) {
  const TLSControl = (await import('./TLSControl.svelte')).default;
  return render(TLSControl, { site, toggleable });
}

describe('TLSControl', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    toggleTLS.mockResolvedValue({ ok: true });
    loadSites.mockResolvedValue(undefined);
  });
  afterEach(() => vi.useRealTimers());

  // The gate is the reason this component exists. Five failed validations lock
  // a domain out for an hour, so a click that could not have worked must not
  // reach the authority.
  it('disables the button while DNS points elsewhere', async () => {
    tlsStatus.mockResolvedValue({
      ready: false,
      message: 'Waiting for DNS — example.com currently resolves to 1.2.3.4, this server is 5.6.7.8',
      issuer: 'letsencrypt'
    });
    await mount();

    const button = screen.getByRole('button');
    await waitFor(() => expect(button).toBeDisabled());
    expect(button.getAttribute('data-tls-gated')).toBe('true');
  });

  it('enables the button once every domain resolves here', async () => {
    tlsStatus.mockResolvedValue({ ready: true, message: '', issuer: 'letsencrypt' });
    await mount();

    const button = screen.getByRole('button');
    await waitFor(() => expect(button.getAttribute('data-tls-gated')).toBe('false'));
    expect(button).not.toBeDisabled();
  });

  // A secured site is already proven. Gating "turn HTTPS off" on a DNS check
  // would strand a site whose domain has moved away, which is exactly when an
  // operator needs to turn it off.
  it('never gates a secured site', async () => {
    tlsStatus.mockResolvedValue({ ready: false, message: 'nope', issuer: 'letsencrypt' });
    await mount({ domain: 'example.com', tls: true } as never);

    const button = screen.getByRole('button');
    await waitFor(() => expect(button.getAttribute('data-tls-gated')).toBe('false'));
    expect(button).not.toBeDisabled();
  });

  // A status that could not be read is not a verdict. Blocking on it would
  // strand the operator behind a check that never answered.
  it('leaves the button usable when the check itself fails', async () => {
    tlsStatus.mockRejectedValue(new Error('network'));
    await mount();

    const button = screen.getByRole('button');
    await waitFor(() => expect(button.getAttribute('data-tls-gated')).toBe('false'));
    expect(button).not.toBeDisabled();
  });

  it('surfaces an issuance failure rather than swallowing it', async () => {
    tlsStatus.mockResolvedValue({ ready: true, message: '', issuer: 'letsencrypt' });
    toggleTLS.mockResolvedValue({ ok: false, error: 'the authority could not validate this domain' });
    await mount();

    const button = screen.getByRole('button');
    await waitFor(() => expect(button).not.toBeDisabled());
    button.click();

    await waitFor(() =>
      expect(openErrorModal).toHaveBeenCalledWith('the authority could not validate this domain')
    );
  });

  // A paused site shows its state and offers nothing to click.
  it('renders no button when the site cannot be toggled', async () => {
    tlsStatus.mockResolvedValue({ ready: true, message: '', issuer: 'letsencrypt' });
    await mount(unsecured, false);

    expect(screen.queryByRole('button')).toBeNull();
  });
});
