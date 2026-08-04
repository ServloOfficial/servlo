import { writable } from 'svelte/store';
import { apiFetch } from '$lib/api';
import { loadStatus } from './status';

export const servloStarting = writable<boolean>(false);
export const servloStopping = writable<boolean>(false);

export async function servloStart(): Promise<boolean> {
  servloStarting.set(true);
  try {
    const res = await apiFetch('/api/servlo/start', { method: 'POST' });
    await loadStatus();
    return res.ok;
  } catch {
    return false;
  } finally {
    servloStarting.set(false);
  }
}

export async function servloStop(): Promise<boolean> {
  servloStopping.set(true);
  try {
    const res = await apiFetch('/api/servlo/stop', { method: 'POST' });
    await loadStatus();
    return res.ok;
  } catch {
    return false;
  } finally {
    servloStopping.set(false);
  }
}
