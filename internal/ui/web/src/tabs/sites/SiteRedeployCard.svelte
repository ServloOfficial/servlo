<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { loadSiteRedeploy, streamRedeploy, type DeployDone } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let available = $state(false);
  let commit = $state('');
  let loading = $state(true);
  // Two presses rather than one. Going back a deploy changes what the site
  // serves and cannot put the database back with it, so the second press is
  // where the operator reads that and agrees to it.
  let confirming = $state(false);
  let running = $state(false);
  let output = $state<string[]>([]);
  let result = $state<DeployDone | null>(null);
  let logEl = $state<HTMLPreElement | null>(null);

  $effect(() => {
    void output.length;
    if (logEl) logEl.scrollTop = logEl.scrollHeight;
  });

  async function load() {
    loading = true;
    try {
      const res = await loadSiteRedeploy(site.domain);
      available = res.available;
      commit = (res.commit ?? '').slice(0, 8);
    } catch {
      available = false;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function go() {
    confirming = false;
    running = true;
    output = [];
    result = null;
    try {
      await streamRedeploy(site.domain, (e) => {
        if ('done' in e) result = e;
        else output = [...output, e.line];
      });
    } finally {
      running = false;
      await load();
    }
  }
</script>

{#if !loading}
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
      {m.sites_redeploy_title()}
    </h2>

    {#if !available}
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
        {m.sites_redeploy_none()}
      </p>
    {:else}
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
        {m.sites_redeploy_desc({ commit })}
      </p>
      <p class="mt-2 text-xs text-amber-600 dark:text-amber-400 leading-relaxed">
        {m.sites_redeploy_migrationWarning()}
      </p>

      <div class="mt-4 flex items-center gap-3">
        {#if confirming}
          <button
            type="button"
            onclick={go}
            disabled={running}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
          >
            {m.sites_redeploy_confirm({ commit })}
          </button>
          <button
            type="button"
            onclick={() => (confirming = false)}
            class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
          >
            {m.sites_redeploy_cancel()}
          </button>
        {:else}
          <button
            type="button"
            onclick={() => (confirming = true)}
            disabled={running}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
          >
            {running ? m.sites_redeploy_running() : m.sites_redeploy_action({ commit })}
          </button>
        {/if}
      </div>
    {/if}

    {#if output.length > 0}
      <pre
        bind:this={logEl}
        class="mt-3 max-h-96 overflow-y-auto rounded-md bg-gray-900 px-3 py-2 font-mono text-xs leading-relaxed text-gray-200 whitespace-pre-wrap">{output.join('\n')}</pre>
    {/if}

    {#if result}
      {#if result.ok}
        <p class="mt-3 text-xs text-green-600 dark:text-green-400">
          {m.sites_redeploy_ok({ commit: (result.to ?? '').slice(0, 8) })}
        </p>
      {:else}
        <p class="mt-3 text-xs text-servlo-red">{result.error}</p>
      {/if}
    {/if}
  </SettingsCard>
{/if}
