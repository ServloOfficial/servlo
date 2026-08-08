import { writable } from 'svelte/store';
import { apiJson } from '$lib/api';

// Whether nginx is bound to the LAN, which is a property of the machine. What
// the caller may do is a matter of their role and lives on the session.
export interface AccessMode {
  lanExposed: boolean;
  checked: boolean;
}

export const accessMode = writable<AccessMode>({
  lanExposed: false,
  checked: false
});
interface AccessModeResponse {
  lan_exposed?: boolean;
}

export async function loadAccessMode() {
  try {
    const res = await apiJson<AccessModeResponse>('/api/access-mode');
    accessMode.set({
      lanExposed: Boolean(res.lan_exposed),
      checked: true
    });
  } catch {
    accessMode.update((a) => ({ ...a, checked: true }));
  }
}
