import { writable } from 'svelte/store';
import { apiFetch, decodeJSONResult } from '$lib/api';

export interface SFTPKey {
  site: string;
  label: string;
  type: string;
  fingerprint: string;
  site_path: string;
}

export interface SFTPSite {
  domain: string;
  path: string;
  port: number;
  confined: boolean;
  keys: SFTPKey[];
}

export interface SFTPStatus {
  sites: SFTPSite[];
  user: string;
  installed: boolean;
  drifted: boolean;
  staged_path: string;
  commands: string[];
  error?: string;
}

const empty: SFTPStatus = {
  sites: [],
  user: '',
  installed: false,
  drifted: false,
  staged_path: '',
  commands: []
};

export const sftpStatus = writable<SFTPStatus>(empty);
export const sftpLoaded = writable(false);

export async function loadSFTP(): Promise<void> {
  try {
    const res = await apiFetch('/api/sftp');
    sftpStatus.set((await res.json()) as SFTPStatus);
  } catch {
    sftpStatus.set({ ...empty, error: 'the SFTP status could not be read' });
  } finally {
    sftpLoaded.set(true);
  }
}

export async function authoriseSFTPKey(
  domain: string,
  label: string,
  key: string
): Promise<{ ok?: boolean; error?: string }> {
  const res = await apiFetch('/api/sftp', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ domain, label, key })
  });
  return decodeJSONResult(res);
}

export async function withdrawSFTPKey(
  fingerprint: string
): Promise<{ ok?: boolean; error?: string }> {
  const res = await apiFetch(`/api/sftp/keys/${encodeURIComponent(fingerprint)}`, {
    method: 'DELETE'
  });
  return decodeJSONResult(res);
}
