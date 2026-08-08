<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SettingsNumberField from './SettingsNumberField.svelte';
  import {
    loadSiteNginxSettings,
    saveSiteNginxSettings,
    type Site,
    type ResponseHeader,
    type SiteNginxSettings
  } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    onOpenRaw: () => void;
  }
  let { site, onOpenRaw }: Props = $props();

  let cacheDays = $state<number | null>(null);
  let headers = $state<ResponseHeader[]>([]);
  let ceilings = $state<SiteNginxSettings | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  $effect(() => {
    const domain = site.domain;
    loading = true;
    error = '';
    loadSiteNginxSettings(domain)
      .then((s) => {
        ceilings = s;
        cacheDays = s.static_cache_days || null;
        headers = s.response_headers ?? [];
      })
      .catch(() => (error = m.sites_phpSettings_loadFailed()))
      .finally(() => (loading = false));
  });

  async function save() {
    saving = true;
    saved = false;
    error = '';
    const res = await saveSiteNginxSettings(site.domain, {
      static_cache_days: cacheDays == null || !Number.isFinite(cacheDays) ? 0 : Math.trunc(cacheDays),
      // A row the operator started and left blank is not a header; sending it
      // would fail validation on a name they never meant to add.
      response_headers: headers.filter((h) => h.name.trim() !== '')
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

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_nginxSettings_title()}
  </h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.sites_nginxSettings_desc()}
  </p>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400">…</p>
  {:else}
    <div class="mt-4">
      <SettingsNumberField
        label={m.sites_nginxSettings_cache()}
        hint={m.sites_nginxSettings_cacheHint()}
        unit={m.sites_nginxSettings_days()}
        max={ceilings?.static_cache_ceiling_days}
        bind:value={cacheDays}
      />
    </div>

    <div class="mt-5">
      <div class="text-xs font-medium text-gray-700 dark:text-gray-200">
        {m.sites_nginxSettings_headers()}
      </div>
      <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
        {m.sites_nginxSettings_headersHint()}
      </p>
      <div class="mt-2 space-y-2">
        {#each headers as header, i (i)}
          <div class="flex items-center gap-2">
            <input
              type="text"
              aria-label="{m.sites_nginxSettings_headerName()} {i + 1}"
              placeholder={m.sites_nginxSettings_headerName()}
              bind:value={header.name}
              class="flex-1 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
            />
            <input
              type="text"
              aria-label="{m.sites_nginxSettings_headerValue()} {i + 1}"
              placeholder={m.sites_nginxSettings_headerValue()}
              bind:value={header.value}
              class="flex-1 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
            />
            <button
              type="button"
              aria-label="{m.sites_nginxSettings_removeHeader()} {i + 1}"
              onclick={() => (headers = headers.filter((_, j) => j !== i))}
              class="px-2 py-1 rounded-md text-xs text-gray-400 hover:text-servlo-red transition-colors"
            >
              &times;
            </button>
          </div>
        {/each}
      </div>
      <button
        type="button"
        onclick={() => (headers = [...headers, { name: '', value: '' }])}
        class="mt-2 text-xs text-servlo-red hover:text-servlo-redhov transition-colors"
      >
        {m.sites_nginxSettings_addHeader()}
      </button>
    </div>

    <div class="mt-4 flex items-center gap-3">
      <button
        type="button"
        onclick={save}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_phpSettings_saving() : m.sites_nginxSettings_saveNginx()}
      </button>
      {#if saved}
        <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
      {/if}
    </div>
  {/if}

  {#if error}
    <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{error}</p>
  {/if}

  <div class="mt-5 pt-4 border-t border-gray-100 dark:border-servlo-border">
    <div class="text-xs font-medium text-gray-700 dark:text-gray-200">
      {m.sites_nginxSettings_raw()}
    </div>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {m.sites_nginxSettings_rawHint()}
    </p>
    <button
      type="button"
      onclick={onOpenRaw}
      class="mt-2 text-xs text-servlo-red hover:text-servlo-redhov transition-colors"
    >
      {m.sites_nginxSettings_rawOpen()}
    </button>
  </div>
</SettingsCard>
