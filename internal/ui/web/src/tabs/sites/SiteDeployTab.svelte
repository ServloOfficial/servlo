<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SiteDeployScriptCard from './SiteDeployScriptCard.svelte';
  import SiteDeployExcludeCard from './SiteDeployExcludeCard.svelte';
  import { streamDeploy, type DeployDone } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let running = $state(false);
  let output = $state<string[]>([]);
  let result = $state<DeployDone | null>(null);
  let logEl = $state<HTMLPreElement | null>(null);

  // A deploy is minutes of output, so the view follows it rather than making
  // the operator scroll to find out what phase it is in.
  $effect(() => {
    void output.length;
    if (logEl) logEl.scrollTop = logEl.scrollHeight;
  });

  async function deploy() {
    running = true;
    output = [];
    result = null;
    try {
      await streamDeploy(site.domain, (e) => {
        if ('done' in e) {
          result = e;
        } else {
          output = [...output, e.line];
        }
      });
    } finally {
      running = false;
    }
  }

  const short = (c?: string) => (c ? c.slice(0, 8) : '');
</script>

<div class="flex-1 overflow-y-auto px-6 py-4 space-y-4">
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
      {m.sites_deploy_title()}
    </h2>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
      {m.sites_deploy_desc()}
    </p>

    <div class="mt-4">
      <button
        type="button"
        onclick={deploy}
        disabled={running}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {running ? m.sites_deploy_running() : m.sites_deploy_action()}
      </button>
    </div>

    {#if output.length > 0}
      <pre
        bind:this={logEl}
        class="mt-3 max-h-96 overflow-y-auto rounded-md bg-gray-900 px-3 py-2 font-mono text-xs leading-relaxed text-gray-200 whitespace-pre-wrap">{output.join('\n')}</pre>
    {/if}

    {#if result}
      {#if result.ok}
        <p class="mt-3 text-xs text-green-600 dark:text-green-400">
          {m.sites_deploy_ok({ commit: short(result.to) })}
        </p>
      {:else}
        <p class="mt-3 text-xs text-servlo-red">{result.error}</p>
        {#if result.to && result.to !== result.from}
          <!-- The pull had already landed. The site is on code the script never
               prepared, and PHP was deliberately not reloaded, so visitors are
               still on the last version that worked. -->
          <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
            {m.sites_deploy_pulledNotLive({ commit: short(result.to) })}
          </p>
        {/if}
      {/if}
      {#if result.snapshot}
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {m.sites_deploy_snapshot({ name: result.snapshot })}
        </p>
      {/if}
    {/if}
  </SettingsCard>

  <SiteDeployScriptCard {site} />
  <SiteDeployExcludeCard {site} />
</div>
