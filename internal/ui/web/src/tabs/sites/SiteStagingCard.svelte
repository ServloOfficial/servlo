<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import Icon from '$components/Icon.svelte';
  import {
    loadSiteStaging,
    refreshStaging,
    resetStagingPassword,
    type SiteStaging
  } from '$stores/staging';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  // One card, two sides of the same relationship. On a staging site it is the
  // controls; on a live site it is a list of the copies that exist, which is
  // the thing somebody about to deploy actually wants to know.

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let info = $state<SiteStaging | null>(null);
  let loading = $state(true);
  let busy = $state('');
  let notice = $state('');
  let error = $state('');
  // Shown once, after a reset, and never fetched again. Nothing stores it.
  let password = $state('');

  async function load() {
    loading = true;
    info = await loadSiteStaging(site.domain);
    loading = false;
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function refresh(bring: string) {
    busy = 'refresh';
    notice = '';
    error = '';
    password = '';
    const res = await refreshStaging(site.domain, bring);
    busy = '';
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    notice = m.staging_refreshed({ files: res.files ?? 0, database: res.database || '—' });
    if (res.note) notice += ' ' + res.note;
    await load();
  }

  async function reset() {
    busy = 'password';
    notice = '';
    error = '';
    const res = await resetStagingPassword(site.domain);
    busy = '';
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    password = res.password ?? '';
    await load();
  }

  function when(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }
</script>

{#if loading}
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.staging_title()}</h2>
    <p class="mt-4 text-xs text-gray-400 dark:text-gray-500">{m.common_loading()}</p>
  </SettingsCard>
{:else if info?.staging}
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.staging_title()}</h2>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.staging_isCopyDesc()}</p>

    <div class="mt-4 space-y-1">
      <div class="flex items-baseline gap-2 flex-wrap">
        <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.staging_copiesFrom()}</span>
        <span class="font-mono text-xs text-gray-800 dark:text-gray-100">{info.origin_domain || info.origin || '—'}</span>
        {#if info.origin && !info.origin_exists}
          <span
            class="text-[11px] px-1.5 py-0.5 rounded bg-amber-50 dark:bg-amber-500/10 text-amber-600 dark:text-amber-400"
            >{m.staging_originGone()}</span
          >
        {/if}
      </div>
      <div class="flex items-baseline gap-2 flex-wrap">
        <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.staging_lastRefreshed()}</span>
        <span class="text-xs text-gray-600 dark:text-gray-300">
          {info.refreshed_at ? when(info.refreshed_at) : m.staging_never()}
        </span>
      </div>
      <div class="flex items-baseline gap-2 flex-wrap">
        <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.staging_username()}</span>
        <span class="font-mono text-xs text-gray-800 dark:text-gray-100">{info.user || '—'}</span>
      </div>
    </div>

    {#if password}
      <!-- The only time this is ever on screen. -->
      <div
        class="mt-4 px-3 py-2 rounded-sm bg-emerald-50 dark:bg-emerald-500/10 border border-emerald-200 dark:border-emerald-500/30"
      >
        <p class="text-xs text-emerald-800 dark:text-emerald-300">{m.staging_newPassword()}</p>
        <p class="mt-1 font-mono text-xs text-emerald-900 dark:text-emerald-200 break-all select-all">{password}</p>
        <p class="mt-1 text-[11px] text-emerald-700 dark:text-emerald-400">{m.staging_shownOnce()}</p>
      </div>
    {/if}

    {#if notice}
      <p class="mt-4 text-xs text-gray-600 dark:text-gray-300">{notice}</p>
    {/if}
    {#if error}
      <p class="mt-4 text-xs text-red-600 dark:text-red-400">{error}</p>
    {/if}

    <div class="mt-4 flex flex-wrap gap-2">
      <DetailButton
        tone="primary"
        onclick={() => refresh('')}
        loading={busy === 'refresh'}
        disabled={busy !== '' || !info.origin_exists}
      >
        {m.staging_refresh()}
      </DetailButton>
      <DetailButton onclick={() => refresh('files')} disabled={busy !== '' || !info.origin_exists}>
        {m.staging_refreshFiles()}
      </DetailButton>
      <DetailButton onclick={reset} loading={busy === 'password'} disabled={busy !== ''}>
        {m.staging_resetPassword()}
      </DetailButton>
    </div>

    <div
      class="mt-4 flex items-start gap-2 px-3 py-2 rounded-sm bg-gray-50 dark:bg-white/3 border border-gray-200 dark:border-servlo-border"
    >
      <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-gray-400 dark:text-gray-500" />
      <p class="text-xs text-gray-600 dark:text-gray-300 leading-relaxed">{m.staging_oneDirection()}</p>
    </div>
  </SettingsCard>
{:else if info && info.copies.length > 0}
  <!-- The live side. Only rendered when copies exist: an empty "no staging
       sites" card on every site on the server is the dashboard clutter
       CLAUDE.md section 8 rules out. -->
  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.staging_title()}</h2>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.staging_hasCopiesDesc()}</p>
    <ul class="mt-4 space-y-1">
      {#each info.copies as domain (domain)}
        <li class="font-mono text-xs text-gray-800 dark:text-gray-100">{domain}</li>
      {/each}
    </ul>
  </SettingsCard>
{/if}
