<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { session } from '$stores/session';
  import { apiJson } from '$lib/api';

  type Entry = {
    at: string;
    action: string;
    subject?: string;
    actor?: string;
    ip?: string;
    result?: string;
    detail?: string;
  };

  let entries = $state<Entry[]>([]);
  let error = $state('');
  let loaded = $state(false);

  // Admin only, and the panel does not ask for it otherwise. The route refuses
  // a developer anyway; not asking keeps a 403 out of their console on every
  // page load.
  const mayRead = $derived($session.role === 'admin');

  async function load() {
    if (!mayRead) return;
    try {
      const data = await apiJson<{ entries: Entry[] }>('/api/audit?limit=100');
      entries = data.entries ?? [];
      error = '';
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not read the audit log.';
    }
    loaded = true;
  }

  $effect(() => {
    if (mayRead && !loaded) load();
  });

  function when(at: string): string {
    const d = new Date(at);
    return Number.isNaN(d.getTime()) ? at : d.toLocaleString();
  }
</script>

{#if mayRead}
  <SettingsCard>
    <div class="flex items-center justify-between mb-2">
      <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Audit log</span>
      <button
        onclick={load}
        class="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100"
      >
        Refresh
      </button>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">
      Everything that changed on this machine, and who changed it. Appended, never rewritten.
    </p>

    {#if error}
      <p class="mt-3 text-xs text-red-500" data-audit-error>{error}</p>
    {:else if entries.length === 0}
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {loaded ? 'Nothing has changed yet.' : 'Reading…'}
      </p>
    {:else}
      <div class="mt-3 max-h-80 overflow-y-auto" data-audit-entries>
        <table class="w-full text-xs">
          <tbody>
            {#each entries as entry (entry.at + entry.action + (entry.subject ?? ''))}
              <tr class="border-b border-gray-100 dark:border-servlo-border/60 last:border-0">
                <td class="py-1.5 pr-3 whitespace-nowrap text-gray-400 dark:text-gray-500">
                  {when(entry.at)}
                </td>
                <td class="py-1.5 pr-3 font-mono text-gray-800 dark:text-gray-200">{entry.action}</td>
                <td class="py-1.5 pr-3 text-gray-600 dark:text-gray-400">{entry.subject ?? ''}</td>
                <td class="py-1.5 pr-3 text-gray-600 dark:text-gray-400">
                  {entry.actor ?? 'servlo'}{#if entry.ip}<span class="text-gray-400 dark:text-gray-500"> · {entry.ip}</span>{/if}
                </td>
                <td class="py-1.5 whitespace-nowrap">
                  {#if entry.result === 'failed'}
                    <span class="text-red-500">failed</span>
                  {:else}
                    <span class="text-gray-400 dark:text-gray-500">ok</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </SettingsCard>
{/if}
