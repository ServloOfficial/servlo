<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { loadSiteDeployHistory, type DeployHistoryEntry } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    // Bumped by the tab after a deploy or a redeploy, so the list reflects what
    // just happened without the operator reloading the page.
    reload?: number;
  }
  let { site, reload = 0 }: Props = $props();

  let entries = $state<DeployHistoryEntry[]>([]);
  let loading = $state(true);
  let error = $state('');

  $effect(() => {
    void site.domain;
    void reload;
    loading = true;
    error = '';
    loadSiteDeployHistory(site.domain)
      .then((e) => (entries = e))
      .catch(() => (error = m.sites_deployHistory_loadFailed()))
      .finally(() => (loading = false));
  });

  const short = (c?: string) => (c ? c.slice(0, 8) : '');

  function when(at: string): string {
    const d = new Date(at);
    return Number.isNaN(d.getTime()) ? at : d.toLocaleString();
  }

  function took(ms?: number): string {
    if (!ms) return '';
    return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
  }
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_deployHistory_title()}
  </h2>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400">…</p>
  {:else if error}
    <p class="mt-3 text-xs text-servlo-red">{error}</p>
  {:else if entries.length === 0}
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
      {m.sites_deployHistory_empty()}
    </p>
  {:else}
    <ul class="mt-3 divide-y divide-gray-100 dark:divide-gray-800">
      {#each entries as e (e.at + (e.to ?? ''))}
        <li class="py-2.5 first:pt-0 last:pb-0">
          <div class="flex items-baseline gap-2 flex-wrap">
            <span
              class="text-xs font-medium {e.ok
                ? 'text-green-600 dark:text-green-400'
                : 'text-servlo-red'}"
            >
              {e.ok
                ? e.redeploy
                  ? m.sites_deployHistory_wentBack()
                  : m.sites_deployHistory_ok()
                : m.sites_deployHistory_failed()}
            </span>
            {#if e.to}
              <code class="text-xs font-mono text-gray-700 dark:text-gray-300">{short(e.to)}</code>
            {/if}
            {#if e.subject}
              <span class="text-xs text-gray-700 dark:text-gray-300">{e.subject}</span>
            {/if}
          </div>

          <div class="mt-0.5 flex items-baseline gap-x-3 gap-y-0.5 flex-wrap text-xs text-gray-500 dark:text-gray-400">
            <span>{when(e.at)}</span>
            {#if e.author}
              <span>{m.sites_deployHistory_by({ author: e.author })}</span>
            {/if}
            <span>
              {e.actor
                ? m.sites_deployHistory_triggeredBy({ actor: e.actor })
                : m.sites_deployHistory_automatic()}
            </span>
            {#if took(e.duration_ms)}
              <span>{took(e.duration_ms)}</span>
            {/if}
            {#if e.kept}
              <span>{m.sites_deployHistory_kept({ count: String(e.kept) })}</span>
            {/if}
            {#if e.snapshot}
              <span>{m.sites_deployHistory_snapshot({ name: e.snapshot })}</span>
            {/if}
          </div>

          {#if !e.ok && e.error}
            <p class="mt-1 text-xs text-servlo-red break-words">{e.error}</p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</SettingsCard>
