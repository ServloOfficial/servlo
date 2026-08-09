import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import { m } from '../paraglide/messages.js';

// Outgoing mail, for a site and for the panel.
//
// Servlo runs no mail server, so both are an account somewhere else. The two
// halves are the same seven fields over two endpoints, which is why one store
// and one form serve both.
//
// The password is write-only. It goes up and never comes back; what comes back
// is has_password, which is all the form needs to offer "leave blank to keep
// the current password".

export type SMTPEncryption = 'none' | 'starttls' | 'tls';

export interface SMTPSettings {
  host?: string;
  port?: number;
  username?: string;
  has_password?: boolean;
  encryption?: SMTPEncryption;
  from_address?: string;
  from_name?: string;
}

export interface SMTPStatus {
  configured: boolean;
  settings: SMTPSettings;
  /** The env keys a save rewrites. Site card only; the panel writes no file. */
  env_keys?: string[];
  /** The file those keys land in (.env, wp-config.php, app/etc/env.php). */
  env_file?: string;
  /** Why servlo cannot wire this site's env file, when it cannot. */
  note?: string;
}

export interface SMTPForm {
  host: string;
  port: number;
  username: string;
  /** Empty means "keep the stored one". */
  password: string;
  encryption: SMTPEncryption;
  from_address: string;
  from_name: string;
}

export interface SMTPResult {
  ok: boolean;
  env_keys?: string[];
  warning?: string;
  to?: string;
  error?: string;
}

const empty: SMTPStatus = { configured: false, settings: {} };

async function get(url: string): Promise<SMTPStatus> {
  try {
    return await apiJson<SMTPStatus>(url);
  } catch {
    return empty;
  }
}

async function send(url: string, method: string, body?: unknown): Promise<SMTPResult> {
  try {
    const res = await apiFetch(url, {
      method,
      ...(body === undefined ? {} : { body: JSON.stringify(body) })
    });
    const data = await decodeJSONResult<SMTPResult>(res);
    if (data.error) return { ok: false, error: data.error };
    return { ok: Boolean(data.ok), env_keys: data.env_keys, warning: data.warning, to: data.to };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

const siteURL = (domain: string) => `/api/sites/${encodeURIComponent(domain)}/smtp`;

export const loadSiteSMTP = (domain: string) => get(siteURL(domain));
export const saveSiteSMTP = (domain: string, form: SMTPForm) =>
  send(siteURL(domain), 'POST', form);
export const removeSiteSMTP = (domain: string) => send(siteURL(domain), 'DELETE');
export const testSiteSMTP = (domain: string, to: string) =>
  send(`${siteURL(domain)}/test`, 'POST', { to });

export const loadPanelSMTP = () => get('/api/settings/smtp');
export const savePanelSMTP = (form: SMTPForm) => send('/api/settings/smtp', 'POST', form);
export const removePanelSMTP = () => send('/api/settings/smtp', 'DELETE');
export const testPanelSMTP = (to: string) => send('/api/settings/smtp/test', 'POST', { to });

/** blankForm is the starting state, and what a delete resets the fields to. */
export function blankForm(): SMTPForm {
  return {
    host: '',
    // 587 with STARTTLS is what almost every provider documents first.
    port: 587,
    username: '',
    password: '',
    encryption: 'starttls',
    from_address: '',
    from_name: ''
  };
}

/** formFrom fills the form from what the server returned, password excluded. */
export function formFrom(settings: SMTPSettings): SMTPForm {
  const blank = blankForm();
  return {
    host: settings.host ?? '',
    port: settings.port || blank.port,
    username: settings.username ?? '',
    password: '',
    encryption: settings.encryption ?? blank.encryption,
    from_address: settings.from_address ?? '',
    from_name: settings.from_name ?? ''
  };
}
