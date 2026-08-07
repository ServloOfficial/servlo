// One origin, always. The dashboard used to call https://localhost:7073
// directly when it was served from servlo.localhost, which made every API call
// cross-origin; a session cookie is SameSite=Strict and would never be attached
// to one of those. The servlo.localhost vhost proxies /api/ instead.
export const apiBase = '';

// csrfToken is handed out by /api/auth/session and /api/auth/login, bound to
// the session it belongs to. The session store sets it; nothing else should.
let csrfToken = '';

export function setCSRFToken(token: string) {
  csrfToken = token;
}

export function getCSRFToken(): string {
  return csrfToken;
}

export function apiUrl(path: string): string {
  if (path.startsWith('http://') || path.startsWith('https://')) return path;
  return apiBase + path;
}

// State-changing requests carry the session's CSRF token. Reads do not need
// one: a forged GET returns data to the panel, not to whoever forged it.
export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const method = (init?.method ?? 'GET').toUpperCase();
  if (method !== 'GET' && method !== 'HEAD') {
    const headers = new Headers(init?.headers);
    if (!headers.has('X-Servlo-CSRF')) headers.set('X-Servlo-CSRF', csrfToken);
    init = { ...init, headers };
  }
  return fetch(apiUrl(path), { credentials: 'same-origin', ...init });
}

export async function apiJson<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await apiFetch(path, init);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json() as Promise<T>;
}

/**
 * Decode a response that handlers sometimes return as JSON envelopes and
 * sometimes as plain-text error bodies (via http.Error). Returns the parsed
 * JSON when possible; otherwise wraps the text body as { ok:false, error }.
 */
export async function decodeJSONResult<T extends { ok?: boolean; error?: string }>(
  res: Response
): Promise<T> {
  return decodeJSONText<T>(await res.text(), `${res.status} ${res.statusText}`);
}

// decodeJSONText is the body half of decodeJSONResult, split out for callers
// that hold the text without a Response, such as an XHR upload.
export function decodeJSONText<T extends { ok?: boolean; error?: string }>(
  text: string,
  status: string
): T {
  if (text) {
    try {
      return JSON.parse(text) as T;
    } catch {
      /* fall through to text-body envelope */
    }
  }
  const fallback: { ok: boolean; error: string } = { ok: false, error: text.trim() || status };
  return fallback as T;
}

export function wsUrl(path: string): string {
  const u = new URL(apiUrl(path), location.href);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.toString();
}
