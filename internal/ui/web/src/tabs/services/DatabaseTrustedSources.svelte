<script lang="ts">
  import { m } from '../../paraglide/messages.js';

  // The address a managed provider has to be told about, and why.
  //
  // It is the single most useful thing on this card: a provider silently drops
  // the packet from an address its trusted-sources list does not hold, so
  // without this the connection test fails and nothing on screen says why. It
  // is one component because the add form needs it before anything is saved and
  // a failing row needs it after.
  interface Props {
    ips: string[];
    error?: string;
  }
  let { ips, error = '' }: Props = $props();

  let copied = $state(false);
  let resetTimer: ReturnType<typeof setTimeout> | null = null;

  async function copy() {
    try {
      await navigator.clipboard.writeText(ips.join(', '));
      copied = true;
      if (resetTimer) clearTimeout(resetTimer);
      resetTimer = setTimeout(() => (copied = false), 1500);
    } catch {
      /* clipboard refused: the address is on screen either way */
    }
  }
</script>

{#if error}
  <p class="text-[11px] text-amber-600 dark:text-amber-500 leading-relaxed">
    {m.dbconn_trustedUnknown({ error })}
  </p>
{:else if ips.length > 0}
  <div class="rounded-md border border-gray-200 dark:border-servlo-border bg-gray-50 dark:bg-white/3 px-3 py-2">
    <div class="flex items-center justify-between gap-3">
      <div class="min-w-0">
        <p class="text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">
          {m.dbconn_trustedLabel()}
        </p>
        <p class="mt-0.5 font-mono text-xs text-gray-700 dark:text-gray-200 break-all">{ips.join(', ')}</p>
      </div>
      <button
        type="button"
        onclick={copy}
        class="shrink-0 text-[11px] font-medium text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
      >
        {#if copied}
          <span class="text-emerald-600 dark:text-emerald-500">{m.common_copied()}</span>
        {:else}
          {m.common_copy()}
        {/if}
      </button>
    </div>
    <p class="mt-1.5 text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{m.dbconn_trustedNote()}</p>
  </div>
{/if}
