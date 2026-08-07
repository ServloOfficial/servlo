import { m } from '../paraglide/messages.js';
import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

export interface LANStatus {
  exposed: boolean;
  lanIP: string;
  macos: boolean;
  loaded: boolean;
  progressSteps: string[];
  loading: boolean;
  error: string;
  justExposed: boolean;
}

const empty: LANStatus = {
  exposed: false,
  lanIP: '',
  macos: false,
  loaded: false,
  progressSteps: [],
  loading: false,
  error: '',
  justExposed: false
};

export const lan = writable<LANStatus>(empty);

function patch(up: Partial<LANStatus>) {
  lan.update((v) => ({ ...v, ...up }));
}

interface StatusResponse {
  exposed?: boolean;
  lan_ip?: string;
  macos?: boolean;
}

interface LANActionEvent {
  step?: string;
  result?: string;
  exposed?: boolean;
  lan_ip?: string;
  error?: string;
}

async function readLANActionResponse(
  response: Response,
  fallbackError: string,
  onStep?: (step: string) => void
): Promise<LANActionEvent> {
  if (!response.ok || !response.body) {
    const text = (await response.text()).trim();
    throw new Error(text || fallbackError);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let finalEvent: LANActionEvent | null = null;
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let newline: number;
    while ((newline = buffer.indexOf('\n')) !== -1) {
      const line = buffer.slice(0, newline).trim();
      buffer = buffer.slice(newline + 1);
      if (!line) continue;
      try {
        const event = JSON.parse(line) as LANActionEvent;
        if (event.step) onStep?.(event.step);
        if (event.result) finalEvent = event;
      } catch {
        /* malformed progress event, skip */
      }
    }
  }
  if (!finalEvent || finalEvent.result === 'error') {
    throw new Error(finalEvent?.error || fallbackError);
  }
  return finalEvent;
}

export async function loadLANStatus() {
  try {
    const data = await apiJson<StatusResponse>('/api/lan/status');
    patch({
      exposed: Boolean(data.exposed),
      lanIP: data.lan_ip || '',
      macos: Boolean(data.macos),
      loaded: true
    });
  } catch {
    patch({ loaded: true });
  }
}

export async function toggleLAN(action: 'expose' | 'unexpose') {
  patch({ loading: true, error: '', progressSteps: [] });
  try {
    const response = await apiFetch('/api/lan/status', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action })
    });
    const finalEvent = await readLANActionResponse(
      response,
      'Toggle failed without a final result',
      (step) => lan.update((state) => ({ ...state, progressSteps: [...state.progressSteps, step] }))
    );
    patch({
      loading: false,
      exposed: Boolean(finalEvent.exposed),
      lanIP: finalEvent.lan_ip || '',
      justExposed: action === 'expose'
    });
  } catch (error) {
    patch({
      loading: false,
      error: error instanceof Error ? error.message : m.system_lan_toggleFailed()
    });
  }
}
