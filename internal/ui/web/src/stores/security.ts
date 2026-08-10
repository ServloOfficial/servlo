import { writable, get } from 'svelte/store';
import { apiFetch } from '$lib/api';

export interface Finding {
  severity: 'bad' | 'warn' | 'ok';
  title: string;
  detail?: string;
  /** Commands for a person to run. Empty when there is nothing to do. */
  fix?: string[];
}

export interface FirewallPlan {
  why: string;
  commands: string[];
  open: number[];
  /** Listening ports the plan does not account for: a rule is a claim, an open
   *  socket is what is true, and the gap is the thing worth acting on. */
  unexpected: number[];
}

export interface Fail2ban {
  running: boolean;
  status_command: string;
  install_commands?: string[];
  why: string;
}

export interface Provider {
  name?: string;
  firewall_url?: string;
  detail: string;
}

export interface SSHKey {
  type: string;
  comment: string;
  fingerprint: string;
  options?: string;
}

export interface Security {
  findings: Finding[];
  firewall: FirewallPlan;
  fail2ban: Fail2ban;
  provider: Provider;
  keys: SSHKey[];
  keys_path: string;
  ssh_port: number;
  error?: string;
}

const empty: Security = {
  findings: [],
  firewall: { why: '', commands: [], open: [], unexpected: [] },
  fail2ban: { running: false, status_command: '', why: '' },
  provider: { detail: '' },
  keys: [],
  keys_path: '',
  ssh_port: 22
};

export const security = writable<Security>(empty);
export const securityLoaded = writable(false);

export async function loadSecurity(): Promise<void> {
  try {
    const res = await apiFetch('/api/security');
    if (!res.ok) return;
    security.set((await res.json()) as Security);
  } catch {
    // Leave the last answer standing. An audit that blanks itself because one
    // request failed reads as a server with nothing wrong.
  } finally {
    securityLoaded.set(true);
  }
}

interface KeyResponse {
  ok: boolean;
  keys: SSHKey[];
  error?: string;
}

async function keyAction(body: Record<string, unknown>): Promise<string | null> {
  try {
    const res = await apiFetch('/api/security/keys', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body)
    });
    const out = (await res.json()) as KeyResponse;
    security.set({ ...get(security), keys: out.keys ?? [] });
    return out.error ?? null;
  } catch (e) {
    return String(e);
  }
}

export const addKey = (key: string, name: string) => keyAction({ action: 'add', key, name });
export const removeKey = (fingerprint: string) => keyAction({ action: 'remove', fingerprint });
