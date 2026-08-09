<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SettingsNumberField from './SettingsNumberField.svelte';
  import SiteNginxSettingsCard from './SiteNginxSettingsCard.svelte';
  import SiteDatabaseCard from './SiteDatabaseCard.svelte';
  import SiteMailCard from './SiteMailCard.svelte';
  import { isAdmin } from '$stores/session';
  import {
    loadSitePHPSettings,
    saveSitePHPSettings,
    type Site,
    type SitePHPSettings
  } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    onOpenRaw: () => void;
  }
  let { site, onOpenRaw }: Props = $props();

  // An empty box is "default", which the server stores as zero. Svelte coerces
  // a type=number binding, so these are numbers and an empty box arrives as
  // null rather than '': anything that reads them has to survive both.
  let upload = $state<number | null>(null);
  let execution = $state<number | null>(null);
  let memory = $state<number | null>(null);
  let ceilings = $state<SitePHPSettings | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  const num = (v: number | null) => (v == null || !Number.isFinite(v) ? 0 : Math.trunc(v));

  $effect(() => {
    const domain = site.domain;
    loading = true;
    error = '';
    loadSitePHPSettings(domain)
      .then((s) => {
        ceilings = s;
        upload = s.max_upload_mb || null;
        execution = s.max_execution_seconds || null;
        memory = s.memory_limit_mb || null;
      })
      .catch(() => (error = m.sites_phpSettings_loadFailed()))
      .finally(() => (loading = false));
  });

  async function save() {
    saving = true;
    saved = false;
    error = '';
    const res = await saveSitePHPSettings(site.domain, {
      max_upload_mb: num(upload),
      max_execution_seconds: num(execution),
      memory_limit_mb: num(memory)
    });
    saving = false;
    if (res.ok) {
      saved = true;
      setTimeout(() => (saved = false), 2000);
    } else {
      error = res.error ?? '';
    }
  }
</script>

<div class="flex-1 overflow-y-auto px-6 py-4 space-y-4">
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
      {m.sites_phpSettings_title()}
    </h2>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
      {m.sites_phpSettings_desc()}
    </p>

    {#if loading}
      <p class="mt-4 text-xs text-gray-400">…</p>
    {:else}
      <div class="mt-4 space-y-4">
        <SettingsNumberField
          label={m.sites_phpSettings_upload()}
          hint={m.sites_phpSettings_uploadHint()}
          unit={m.sites_phpSettings_mb()}
          max={ceilings?.max_upload_ceiling_mb}
          bind:value={upload}
        />
        <SettingsNumberField
          label={m.sites_phpSettings_execution()}
          hint={m.sites_phpSettings_executionHint()}
          unit={m.sites_phpSettings_seconds()}
          max={ceilings?.max_execution_ceiling_s}
          bind:value={execution}
        />
        <SettingsNumberField
          label={m.sites_phpSettings_memory()}
          hint={m.sites_phpSettings_memoryHint()}
          unit={m.sites_phpSettings_mb()}
          max={ceilings?.memory_limit_ceiling_mb}
          bind:value={memory}
        />
      </div>

      <div class="mt-4 flex items-center gap-3">
        <button
          type="button"
          onclick={save}
          disabled={saving}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
        >
          {saving ? m.sites_phpSettings_saving() : m.sites_phpSettings_savePhp()}
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

  <SiteNginxSettingsCard {site} {onOpenRaw} />

  <!-- Connections are an admin read: the API refuses a Developer, so offering
       the picker to one would only ever show them a 403. -->
  {#if $isAdmin}
    <SiteDatabaseCard {site} />
    <SiteMailCard {site} />
  {/if}
</div>
