import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  blankForm,
  formFrom,
  loadSiteSMTP,
  saveSiteSMTP,
  removeSiteSMTP,
  testSiteSMTP,
  loadPanelSMTP,
  savePanelSMTP,
  type SMTPForm
} from './smtp';

function form(over: Partial<SMTPForm> = {}): SMTPForm {
  return { ...blankForm(), host: 'smtp.example.com', from_address: 'a@b.test', ...over };
}

describe('smtp store', () => {
  let calls: Array<[string, RequestInit | undefined]>;

  beforeEach(() => {
    calls = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response(JSON.stringify({ ok: true, env_keys: ['MAIL_HOST'] }), { status: 200 });
    }) as unknown as typeof fetch;
  });

  it('addresses the site endpoint with the domain escaped', async () => {
    await saveSiteSMTP('blog.orbitlabs.app', form());
    expect(calls[0][0]).toContain('/api/sites/blog.orbitlabs.app/smtp');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body)).host).toBe('smtp.example.com');
  });

  it('sends the test to its own endpoint', async () => {
    await testSiteSMTP('acme-supply.com', 'ops@acme-supply.com');
    expect(calls[0][0]).toContain('/api/sites/acme-supply.com/smtp/test');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ to: 'ops@acme-supply.com' });
  });

  it('removes with DELETE and no body', async () => {
    await removeSiteSMTP('acme-supply.com');
    expect(calls[0][1]?.method).toBe('DELETE');
    expect(calls[0][1]?.body).toBeUndefined();
  });

  it('uses the panel endpoints for the panel account', async () => {
    await savePanelSMTP(form());
    expect(calls[0][0]).toContain('/api/settings/smtp');
    expect(calls[0][0]).not.toContain('/api/sites/');
  });

  it('reports the server error rather than a generic failure', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: '550 5.7.1 Sender address not verified' }), {
          status: 502
        })
    ) as unknown as typeof fetch;

    const res = await saveSiteSMTP('acme-supply.com', form());
    expect(res.ok).toBe(false);
    expect(res.error).toContain('Sender address not verified');
  });

  it('falls back to the empty state when the account cannot be read', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('offline');
    }) as unknown as typeof fetch;

    expect(await loadSiteSMTP('acme-supply.com')).toEqual({ configured: false, settings: {} });
    expect(await loadPanelSMTP()).toEqual({ configured: false, settings: {} });
  });
});

describe('formFrom', () => {
  // The server never sends the password back, so the field always starts empty
  // and an untouched save means "keep the stored one".
  it('never carries a password and keeps the rest', () => {
    const f = formFrom({
      host: 'smtp.eu.mailgun.org',
      port: 465,
      username: 'postmaster@mg.acme',
      has_password: true,
      encryption: 'tls',
      from_address: 'orders@acme-supply.com',
      from_name: 'Acme Supply Co.'
    });
    expect(f.password).toBe('');
    expect(f).toMatchObject({
      host: 'smtp.eu.mailgun.org',
      port: 465,
      encryption: 'tls',
      from_name: 'Acme Supply Co.'
    });
  });

  it('defaults an unconfigured account to the port and mode providers document first', () => {
    const f = formFrom({});
    expect(f.port).toBe(587);
    expect(f.encryption).toBe('starttls');
  });
});
