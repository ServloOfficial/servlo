import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import { m } from '../paraglide/messages.js';

// The account a site reaches its database as.
//
// The password is not here and never will be: it lives in the site's env file,
// which is where the application reads it from. What the panel shows is which
// account, on which server, and which keys a rotation rewrites.
export interface SiteDBUser {
  connection: string;
  location: string;
  database: string;
  user: string;
  own_account: boolean;
  admin_user: string;
  env_keys?: string[];
  note?: string;
  error?: string;
}

export interface RotateResult {
  ok: boolean;
  user?: string;
  env_keys?: string[];
  error?: string;
}

export async function loadSiteDBUser(domain: string): Promise<SiteDBUser | null> {
  try {
    return await apiJson<SiteDBUser>(`/api/sites/${encodeURIComponent(domain)}/db-user`);
  } catch {
    return null;
  }
}

export async function rotateSiteDBUser(domain: string): Promise<RotateResult> {
  try {
    const res = await apiFetch(`/api/sites/${encodeURIComponent(domain)}/db-user/rotate`, {
      method: 'POST'
    });
    const data = await decodeJSONResult<RotateResult>(res);
    if (data.error) return { ok: false, error: data.error };
    return { ok: Boolean(data.ok), user: data.user, env_keys: data.env_keys };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}
