<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SettingsNumberField from './SettingsNumberField.svelte';
  import SiteRedirectsCard from './SiteRedirectsCard.svelte';
  import {
    loadSiteNginxSettings,
    saveSiteNginxSettings,
    type Site,
    type ResponseHeader,
    type Redirect,
    type SiteNginxSettings
  } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    onOpenRaw: () => void;
  }
  let { site, onOpenRaw }: Props = $props();

  let cacheDays = $state<number | null>(null);
  let canonical = $state('');
  let headers = $state<ResponseHeader[]>([]);
  let redirectTo = $state('');
  let redirectPermanent = $state(false);
  let redirects = $state<Redirect[]>([]);
  let ceilings = $state<SiteNginxSettings | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');
  // Both cards send one request, so one failure has two places it could be
  // shown and showing it in both reads as two problems. It goes next to the
  // button that was pressed.
  let source = $state<'settings' | 'redirects'>('settings');

  $effect(() => {
    const domain = site.domain;
    loading = true;
    error = '';
    loadSiteNginxSettings(domain)
      .then((s) => {
        ceilings = s;
        cacheDays = s.static_cache_days || null;
        canonical = s.canonical_host ?? '';
        headers = s.response_headers ?? [];
        redirectTo = s.redirect_to ?? '';
        redirectPermanent = Boolean(s.redirect_permanent);
        redirects = s.redirects ?? [];
      })
      .catch(() => (error = m.sites_phpSettings_loadFailed()))
      .finally(() => (loading = false));
  });

  async function save(from: 'settings' | 'redirects' = 'settings') {
    source = from;
    saving = true;
    saved = false;
    error = '';
    const res = await saveSiteNginxSettings(site.domain, {
      static_cache_days: cacheDays == null || !Number.isFinite(cacheDays) ? 0 : Math.trunc(cacheDays),
      // A row the operator started and left blank is not a header; sending it
      // would fail validation on a name they never meant to add.
      response_headers: headers.filter((h) => h.name.trim() !== ''),
      canonical_host: canonical,
      redirect_to: redirectTo.trim(),
      redirect_permanent: redirectPermanent,
      // A row the operator started and abandoned is not a rule; sending it
      // would fail validation on a path they never meant to add.
      redirects: redirects.filter((r) => r.from.trim() !== '' || r.to.trim() !== '')
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
        {m.sites_nginxSettings_canonical()}
      </div>
      <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
        {m.sites_nginxSettings_canonicalHint()}
      </p>
      {#if ceilings?.canonical_available}
        <select
          aria-label={m.sites_nginxSettings_canonical()}
          bind:value={canonical}
          class="mt-2 px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
        >
          <option value="">{m.sites_nginxSettings_canonicalBoth()}</option>
          <option value="apex">{ceilings.apex_host}</option>
          <option value="www">{ceilings.www_host}</option>
        </select>
      {:else}
        <p class="mt-2 text-[11px] text-gray-400 dark:text-gray-500">
          {m.sites_nginxSettings_canonicalUnavailable()}
        </p>
      {/if}
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
        onclick={() => save('settings')}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_phpSettings_saving() : m.sites_nginxSettings_saveNginx()}
      </button>
      {#if saved && source === 'settings'}
        <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
      {/if}
    </div>
  {/if}

  {#if error && source === 'settings'}
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

<div class="mt-4">
  <SiteRedirectsCard
    bind:redirectTo
    bind:redirectPermanent
    bind:redirects
    saving={saving && source === 'redirects'}
    saved={saved && source === 'redirects'}
    error={source === 'redirects' ? error : ''}
    onSave={() => save('redirects')}
  />
</div>
