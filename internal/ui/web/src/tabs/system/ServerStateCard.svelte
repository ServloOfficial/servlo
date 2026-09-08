<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import {
    loadServerState,
    backUpServerState,
    type StateArchive
  } from '$stores/backups';
  import { m } from '../../paraglide/messages.js';

  // Backing up what a site backup does not carry.
  //
  // Restoring a site's archive onto a fresh machine gives you its files and
  // its database and a server with no idea what a site is. This is the other
  // half, and until now it existed only as a command.

  let archives = $state<StateArchive[]>([]);
  let directory = $state('');
  let loading = $state(true);
  let taking = $state(false);
  let error = $state('');
  let justTook = $state('');
  let sendErrors = $state<string[]>([]);

  async function refresh() {
    try {
      const res = await loadServerState();
      if (res.error) {
        error = res.error;
        return;
      }
      archives = res.archives ?? [];
      directory = res.directory ?? '';
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      loading = false;
    }
  }

  async function takeOne() {
    taking = true;
    error = '';
    justTook = '';
    sendErrors = [];
    try {
      const res = await backUpServerState();
      if (!res.ok) {
        error = res.error || m.common_failed();
        return;
      }
      justTook = res.name ?? '';
      // A written archive that reached no destination is still only on this
      // machine, which is the one thing this archive exists to survive.
      sendErrors = res.send_errors ?? [];
      await refresh();
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      taking = false;
    }
  }

  function size(bytes: number): string {
    if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    if (bytes >= 1024) return Math.round(bytes / 1024) + ' KB';
    return bytes + ' B';
  }

  function when(stamp: string): string {
    const d = new Date(stamp);
    return isNaN(d.getTime()) ? stamp : d.toLocaleString();
  }

  $effect(() => {
    refresh();
  });
</script>

<SettingsCard>
  <div class="flex items-center justify-between mb-2">
    <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_state_title()}</span>
    <DetailButton onclick={takeOne} loading={taking}>{m.system_state_take()}</DetailButton>
  </div>

  <p class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.system_state_blurb()}</p>

  {#if !loading}
    {#if archives.length === 0}
      <p class="mt-3 text-xs text-gray-400 dark:text-gray-500">{m.system_state_none()}</p>
    {:else}
      <ul class="mt-3 divide-y divide-gray-100 dark:divide-servlo-border">
        {#each archives as a (a.name)}
          <li class="flex items-baseline gap-3 py-1.5 text-xs">
            <span class="font-mono text-gray-700 dark:text-gray-200 truncate">{a.name}</span>
            <span class="ml-auto shrink-0 text-gray-400 dark:text-gray-500">{size(a.size)}</span>
            <span class="shrink-0 w-40 text-right text-gray-500 dark:text-gray-400">{when(a.taken)}</span>
          </li>
        {/each}
      </ul>
      {#if directory}
        <p class="mt-2 text-[11px] font-mono text-gray-400 dark:text-gray-500 break-all">{directory}</p>
      {/if}
    {/if}
  {/if}

  {#if justTook}
    <p class="mt-2 text-xs text-green-600 dark:text-green-500">{m.system_state_took({ name: justTook })}</p>
  {/if}

  <!-- Boxed, because the standing note about the backup key below is amber too
       and two amber paragraphs in a row read as one. This is a result of the
       click just made; that one is always true. -->
  {#if sendErrors.length}
    <div class="mt-2 rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 p-2.5">
      <p class="text-xs font-medium text-amber-700 dark:text-amber-400">{m.system_state_notSentOffsite()}</p>
      <ul class="mt-1.5 space-y-1">
        {#each sendErrors as reason (reason)}
          <li class="flex gap-1.5 text-[11px] font-mono text-amber-800/80 dark:text-amber-200/70 break-all">
            <span aria-hidden="true" class="shrink-0">&bull;</span><span>{reason}</span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if error}
    <p class="mt-2 text-xs text-red-500">{error}</p>
  {/if}

  <p class="mt-3 text-[11px] text-amber-600 dark:text-amber-500 leading-relaxed">{m.system_state_keyWarning()}</p>
</SettingsCard>
