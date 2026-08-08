<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import {
    loadSiteDeployExclude,
    saveSiteDeployExclude,
    resetSiteDeployExclude
  } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  // One path per line, because that is how an operator thinks about a list of
  // directories and it is the one editor shape that never needs a button to add
  // a row.
  let text = $state('');
  let custom = $state(false);
  let fallback = $state<string[]>([]);
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  const lines = (v: string) =>
    v
      .split('\n')
      .map((s) => s.trim())
      .filter((s) => s !== '');

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await loadSiteDeployExclude(site.domain);
      text = res.paths.join('\n');
      custom = res.custom;
      fallback = res.default ?? [];
    } catch {
      error = m.sites_deployExclude_loadFailed();
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  function flashSaved() {
    saved = true;
    setTimeout(() => (saved = false), 2000);
  }

  async function save() {
    saving = true;
    saved = false;
    error = '';
    const res = await saveSiteDeployExclude(site.domain, lines(text));
    saving = false;
    if (!res.ok) {
      error = res.error ?? '';
      return;
    }
    flashSaved();
    await load();
  }

  async function reset() {
    saving = true;
    saved = false;
    error = '';
    const res = await resetSiteDeployExclude(site.domain);
    saving = false;
    if (!res.ok) {
      error = res.error ?? '';
      return;
    }
    flashSaved();
    await load();
  }
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_deployExclude_title()}
  </h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.sites_deployExclude_desc()}
  </p>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400">…</p>
  {:else}
    <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
      {custom
        ? m.sites_deployExclude_sourceSite()
        : m.sites_deployExclude_sourceFramework()}
    </p>

    <textarea
      bind:value={text}
      spellcheck="false"
      rows="4"
      placeholder={m.sites_deployExclude_placeholder()}
      class="mt-2 w-full rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-800 dark:text-gray-200 focus:outline-none focus:ring-1 focus:ring-servlo-red"
    ></textarea>
    <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
      {m.sites_deployExclude_hint()}
    </p>

    {#if lines(text).length === 0}
      <p class="mt-2 text-xs text-amber-600 dark:text-amber-400">
        {m.sites_deployExclude_emptyWarning()}
      </p>
    {/if}

    <div class="mt-4 flex items-center gap-3">
      <button
        type="button"
        onclick={save}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_phpSettings_saving() : m.sites_deployExclude_save()}
      </button>
      {#if custom}
        <button
          type="button"
          onclick={reset}
          disabled={saving}
          class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 disabled:opacity-50 transition-colors"
        >
          {m.sites_deployExclude_reset({ paths: fallback.join(', ') || m.sites_deployExclude_none() })}
        </button>
      {/if}
      {#if saved}
        <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
      {/if}
    </div>
  {/if}

  {#if error}
    <p class="mt-3 text-xs text-servlo-red">{error}</p>
  {/if}
</SettingsCard>
