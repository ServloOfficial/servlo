import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { get } from 'svelte/store';

import { session, loadSession, signIn, signOut } from './session';
import { getCSRFToken, setCSRFToken } from '$lib/api';

describe('session store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    session.set({
      loaded: false,
      authenticated: false,
      setupNeeded: false,
      codeRequired: false,
      user: '',
      role: '',
      totpEnabled: false,
      recoveryCodesLeft: 0,
      error: '',
      busy: false
    });
    setCSRFToken('');
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('reads who is signed in', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ authenticated: true, user: 'alice', role: 'admin', csrf: 'tok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    ) as unknown as typeof fetch;

    await loadSession();
    expect(get(session)).toMatchObject({ loaded: true, authenticated: true, user: 'alice', role: 'admin' });
  });

  // The CSRF token arrives with the session and every state-changing request
  // carries it, so a store that dropped it would leave the dashboard able to
  // read everything and change nothing.
  it('adopts the CSRF token the panel hands out', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ authenticated: true, user: 'alice', csrf: 'the-token' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    ) as unknown as typeof fetch;

    await loadSession();
    expect(getCSRFToken()).toBe('the-token');
  });

  // A fresh install has no account, and the dashboard has to show the setup
  // form rather than a login form nothing can satisfy.
  it('reports when the panel still needs setting up', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ setup_needed: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    ) as unknown as typeof fetch;

    await loadSession();
    expect(get(session)).toMatchObject({ loaded: true, authenticated: false, setupNeeded: true });
  });

  it('surfaces a rejected sign-in without authenticating', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response('That username and password do not match.\n', { status: 401 })
    ) as unknown as typeof fetch;

    const ok = await signIn('alice', 'wrong');
    expect(ok).toBe(false);
    expect(get(session)).toMatchObject({ authenticated: false, busy: false });
    expect(get(session).error).toContain('do not match');
  });

  // Signing out has to clear the token as well as the flag: leaving it behind
  // would have the next person at the keyboard posting with someone else's.
  it('clears the session and the token on sign-out', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ ok: true }), { status: 200 })
    ) as unknown as typeof fetch;
    session.set({
      loaded: true,
      authenticated: true,
      setupNeeded: false,
      codeRequired: false,
      user: 'alice',
      role: 'admin',
      totpEnabled: false,
      recoveryCodesLeft: 0,
      error: '',
      busy: false
    });
    setCSRFToken('the-token');

    await signOut();
    expect(get(session)).toMatchObject({ authenticated: false, user: '' });
    expect(getCSRFToken()).toBe('');
  });

  // An unreachable panel and an unauthenticated one both lead to the login
  // form; what must not happen is the dashboard rendering as if signed in.
  it('does not authenticate when the panel cannot be reached', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('network down');
    }) as unknown as typeof fetch;

    await loadSession();
    expect(get(session)).toMatchObject({ loaded: true, authenticated: false });
  });
});

// The panel says a code is owed only after a correct password, so the store
// treats that message as the cue to show the field rather than guessing at
// what went wrong.
describe('session store and the second factor', () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('asks for a code when the panel says one is needed', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response('That account needs a code from its authenticator app.\n', { status: 401 })
    ) as unknown as typeof fetch;

    const ok = await signIn('alice', 'a long enough passphrase');
    expect(ok).toBe(false);
    expect(get(session).codeRequired).toBe(true);
  });

  it('does not ask for a code on an ordinary rejection', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response('That username and password do not match.\n', { status: 401 })
    ) as unknown as typeof fetch;

    await signIn('alice', 'wrong');
    expect(get(session).codeRequired).toBe(false);
  });

  it('sends the code along with the password', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(JSON.parse(String(init?.body))).toEqual({
        username: 'alice',
        password: 'a long enough passphrase',
        code: '123456'
      });
      return new Response(JSON.stringify({ authenticated: true, user: 'alice' }), { status: 200 });
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    await signIn('alice', 'a long enough passphrase', '123456');
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('carries whether the account has a second factor on', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({ authenticated: true, user: 'alice', totp_enabled: true, recovery_codes: 7 }),
        { status: 200 }
      )
    ) as unknown as typeof fetch;

    await loadSession();
    expect(get(session)).toMatchObject({ totpEnabled: true, recoveryCodesLeft: 7 });
  });
});
