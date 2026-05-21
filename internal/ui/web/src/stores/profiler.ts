import { writable } from 'svelte/store';
import { apiFetch, apiJson } from '$lib/api';

// profilerEnabled mirrors the global SPX profiler toggle.
export const profilerEnabled = writable<boolean>(false);

export async function loadProfilerStatus(): Promise<void> {
  try {
    const s = await apiJson<{ enabled: boolean }>('/api/profiler/status');
    profilerEnabled.set(Boolean(s.enabled));
  } catch {
    /* keep previous value */
  }
}

// setProfiler turns the global SPX profiler on or off. On means every
// PHP-FPM site's requests are profiled.
export async function setProfiler(enable: boolean): Promise<void> {
  const res = await apiFetch('/api/profiler/toggle', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enable })
  });
  if (!res.ok) {
    throw new Error((await res.text()) || `profiler toggle failed (${res.status})`);
  }
  const data = (await res.json()) as { enabled: boolean };
  profilerEnabled.set(Boolean(data.enabled));
}
