import { apiFetch } from '$lib/api';

export interface SiteStaging {
  staging: boolean;
  origin?: string;
  /** The live site's address, which is what an operator recognises. The name
   *  above is servlo's internal one, and all that is left when it is gone. */
  origin_domain?: string;
  /** False when the live site has been removed, which turns the refresh off
   *  rather than letting it fail. */
  origin_exists: boolean;
  user?: string;
  refreshed_at?: string;
  /** For a live site: the staging sites that copy from it. */
  copies: string[];
  error?: string;
}

export interface StagingActionResult {
  ok: boolean;
  error?: string;
  files?: number;
  bytes?: number;
  database?: string;
  /** Returned exactly once, by a reset, and stored nowhere. */
  password?: string;
  user?: string;
  note?: string;
}

const base = (domain: string) => `/api/sites/${encodeURIComponent(domain)}/staging`;

export async function loadSiteStaging(domain: string): Promise<SiteStaging | null> {
  try {
    const res = await apiFetch(base(domain));
    if (!res.ok) return null;
    return (await res.json()) as SiteStaging;
  } catch {
    return null;
  }
}

async function post(domain: string, body: Record<string, unknown>): Promise<StagingActionResult> {
  try {
    const res = await apiFetch(base(domain), {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body)
    });
    return (await res.json()) as StagingActionResult;
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

/** bring is 'files', 'database', or empty for both. */
export const refreshStaging = (domain: string, bring = '') =>
  post(domain, { action: 'refresh', bring });
export const resetStagingPassword = (domain: string) => post(domain, { action: 'password' });
