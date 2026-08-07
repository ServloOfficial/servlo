import { writable, get } from 'svelte/store';
import { apiFetch, setCSRFToken } from '$lib/api';

export type SessionState = {
  loaded: boolean;
  authenticated: boolean;
  setupNeeded: boolean;
  user: string;
  role: string;
  error: string;
  busy: boolean;
};

const empty: SessionState = {
  loaded: false,
  authenticated: false,
  setupNeeded: false,
  user: '',
  role: '',
  error: '',
  busy: false
};

export const session = writable<SessionState>(empty);

function patch(up: Partial<SessionState>) {
  session.update((v) => ({ ...v, ...up }));
}

type SessionResponse = {
  authenticated?: boolean;
  setup_needed?: boolean;
  user?: string;
  role?: string;
  csrf?: string;
};

function adopt(data: SessionResponse) {
  // The CSRF token is bound to this session, so it arrives with the session
  // and goes when the session does.
  setCSRFToken(data.csrf ?? '');
  patch({
    loaded: true,
    authenticated: Boolean(data.authenticated),
    setupNeeded: Boolean(data.setup_needed),
    user: data.user ?? '',
    role: data.role ?? '',
    error: ''
  });
}

/** Ask the panel who we are. Runs before anything else on load. */
export async function loadSession() {
  try {
    const res = await apiFetch('/api/auth/session');
    if (!res.ok) throw new Error(await res.text());
    adopt((await res.json()) as SessionResponse);
  } catch {
    // Unreachable panel and unauthenticated look the same from here, and both
    // lead to the same place: the login form, which will say so when the
    // attempt fails.
    patch({ loaded: true, authenticated: false });
  }
}

async function submit(path: string, username: string, password: string) {
  patch({ busy: true, error: '' });
  try {
    const res = await apiFetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    });
    if (!res.ok) {
      patch({ busy: false, error: (await res.text()).trim() || 'That did not work.' });
      return false;
    }
    adopt({ ...((await res.json()) as SessionResponse), authenticated: true });
    patch({ busy: false });
    return true;
  } catch (error) {
    patch({ busy: false, error: error instanceof Error ? error.message : 'That did not work.' });
    return false;
  }
}

export const signIn = (username: string, password: string) =>
  submit('/api/auth/login', username, password);

export const createFirstAccount = (username: string, password: string) =>
  submit('/api/auth/setup', username, password);

export async function signOut() {
  patch({ busy: true });
  try {
    await apiFetch('/api/auth/logout', { method: 'POST' });
  } catch {
    // The session may already be gone, which is the state the button was
    // asking for. Either way the page goes back to the login form.
  }
  setCSRFToken('');
  session.set({ ...empty, loaded: true });
}

/** True once the panel has answered, whatever the answer was. */
export const sessionLoaded = () => get(session).loaded;
