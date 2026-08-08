<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { loadSiteDeployScript, saveSiteDeployScript } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let body = $state('');
  let path = $state('');
  let exists = $state(false);
  let migrates = $state(false);
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await loadSiteDeployScript(site.domain);
      body = res.body;
      path = res.path;
      exists = res.exists;
      migrates = res.migrates;
    } catch {
      error = m.sites_deployScript_loadFailed();
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function save() {
    saving = true;
    saved = false;
    error = '';
    const res = await saveSiteDeployScript(site.domain, body);
    saving = false;
    if (!res.ok) {
      error = res.error ?? '';
      return;
    }
    saved = true;
    setTimeout(() => (saved = false), 2000);
    await load();
  }
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_deployScript_title()}
  </h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.sites_deployScript_desc()}
  </p>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400">…</p>
  {:else}
    <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
      {exists ? m.sites_deployScript_saved({ path }) : m.sites_deployScript_template({ path })}
    </p>

    <textarea
      bind:value={body}
      spellcheck="false"
      rows="14"
      class="mt-2 w-full rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-800 dark:text-gray-200 focus:outline-none focus:ring-1 focus:ring-servlo-red"
    ></textarea>

    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
      {migrates ? m.sites_deployScript_migrates() : m.sites_deployScript_noMigration()}
    </p>

    <div class="mt-4 flex items-center gap-3">
      <button
        type="button"
        onclick={save}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_phpSettings_saving() : m.sites_deployScript_save()}
      </button>
      {#if saved}
        <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
      {/if}
    </div>
  {/if}

  {#if error}
    <p class="mt-3 text-xs text-servlo-red">{error}</p>
  {/if}
</SettingsCard>
