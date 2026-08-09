import { writable } from 'svelte/store';
import { apiFetch, apiJson, decodeJSONResult } from '$lib/api';
import { m } from '../paraglide/messages.js';

// A database is a connection, either a local service servlo runs or a managed
// server somewhere else. The password is never part of what the panel reads
// back: the API drops it and nothing here puts it in the model.
export interface DBConnection {
  name: string;
  family: string;
  service?: string;
  host?: string;
  port?: number;
  user?: string;
  tls_mode?: string;
  ca_cert?: string;
  default: boolean;
  sites: string[];
}

export interface DBConnectionsState {
  connections: DBConnection[];
  // The local database services installed here, which is what an "add a local
  // connection" form can offer.
  services: string[];
  loading: boolean;
  error: string;
}

interface ConnectionsResponse {
  connections?: DBConnection[];
  services?: string[];
  error?: string;
}

const empty: DBConnectionsState = { connections: [], services: [], loading: false, error: '' };

export const dbConnections = writable<DBConnectionsState>(empty);

function apply(res: ConnectionsResponse): void {
  dbConnections.set({
    connections: res.connections ?? [],
    services: res.services ?? [],
    loading: false,
    error: res.error ?? ''
  });
}

export async function loadDBConnections(): Promise<void> {
  dbConnections.update((s) => ({ ...s, loading: true }));
  try {
    apply(await apiJson<ConnectionsResponse>('/api/db-connections'));
  } catch (e) {
    dbConnections.update((s) => ({
      ...s,
      loading: false,
      error: e instanceof Error ? e.message : m.common_requestFailed()
    }));
  }
}

export interface DBConnectionResult {
  ok: boolean;
  error?: string;
}

// What adding a connection needs. A local one names an installed service; a
// managed one says where it is and what opens it. The password goes out with
// the request and is never read back.
export interface AddConnectionPayload {
  name: string;
  service?: string;
  engine?: string;
  host?: string;
  port?: number;
  user?: string;
  password?: string;
  tls_mode?: string;
  ca_cert?: string;
}

// Every action answers with the whole list, so the store takes its next state
// from the reply rather than firing a second request for it.
async function act(body: Record<string, unknown>): Promise<DBConnectionResult> {
  try {
    const res = await apiFetch('/api/db-connections', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await decodeJSONResult<{
      ok?: boolean;
      error?: string;
      connections?: ConnectionsResponse;
    }>(res);
    // Errors here are written for an operator to read, so they travel to the
    // view as they arrived rather than as a generic failure.
    if (data.error) return { ok: false, error: data.error };
    if (data.connections) apply(data.connections);
    return { ok: Boolean(data.ok) };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : m.common_requestFailed() };
  }
}

export const addConnection = (payload: AddConnectionPayload) => act({ action: 'add', ...payload });

export const removeConnection = (name: string) => act({ action: 'remove', name });

export const setDefaultConnection = (name: string) => act({ action: 'default', name });

// An empty connection puts the site back on the install default.
export const assignConnection = (domain: string, connection: string) =>
  act({ action: 'assign', domain, connection });

export const isLocalConnection = (c: DBConnection) => Boolean(c.service);

// Where a connection is, in the one line a list row has for it: the service
// name when servlo runs it, the address when somebody else does.
export function connectionLocation(c: DBConnection): string {
  if (isLocalConnection(c)) return c.service ?? '';
  if (!c.host) return '';
  return c.port ? `${c.host}:${c.port}` : c.host;
}

// The connection a site is on, or empty when it follows the install default.
// Derived from each connection's site list, because that is what the API says
// and a second copy on the site would be a second thing to keep in step.
export function connectionForSite(connections: DBConnection[], domain: string): string {
  return connections.find((c) => c.sites.includes(domain))?.name ?? '';
}

// The port an engine answers on, shown as a placeholder so an untouched field
// means "the usual one" rather than a number the operator never chose.
export const defaultPortFor = (engine: string) => (engine === 'postgres' ? 5432 : 3306);
