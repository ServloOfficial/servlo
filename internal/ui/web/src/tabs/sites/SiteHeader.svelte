<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Badge from '$components/Badge.svelte';
  import {
    type Site,
    pauseSite,
    resumeSite,
    pinSite,
    unpinSite,
    restartSite,
    openSiteInBrowser,
    openTerminal,
    openFolder,
    loadSites,
  } from '$stores/sites';
  import {
    openDomainModal,
    openErrorModal,
    openGroupModal,
    openSiteUnlinkModal
  } from '$stores/modals';
  import Icon from '$components/Icon.svelte';
  import { tooltip } from '$lib/tooltip';
  import { accessMode } from '$stores/accessMode';
  import { status, loadStatus } from '$stores/status';
  import { apiBase } from '$lib/api';
  import { homeShorten } from '$lib/path';
  import DomainMorePill from './DomainMorePill.svelte';
  import TLSControl from './TLSControl.svelte';
  import WorkspacePicker from './WorkspacePicker.svelte';
  import { m } from '../../paraglide/messages.js';

  import type { Snippet } from 'svelte';

  interface Props {
    site: Site;
    tabs?: Snippet;
    onOpenNginx?: () => void;
  }
  let { site, tabs, onOpenNginx = () => {} }: Props = $props();

  let pauseBusy = $state(false);
  let restartBusy = $state(false);
  let pinBusy = $state(false);

  async function togglePin() {
    pinBusy = true;
    try {
      await (site.pinned ? unpinSite(site.domain) : pinSite(site.domain));
    } finally {
      pinBusy = false;
    }
  }
  let overflowOpen = $state(false);
  let overflowEl: HTMLDivElement | null = $state(null);


  const activeDomain = $derived(site.domain);
  const activePath = $derived(site.path || '');
  const activePathLabel = $derived(homeShorten(activePath, $status.home));
  const activeFrameworkLabel = $derived(site.framework_label);

  const urlEditable = $derived(!site.paused);
  const tlsToggleable = $derived(urlEditable);

  // A host-proxy site's dev server is its only runtime, and restarting it is the
  // routine fix when it wedges, so it gets a first-class header button rather
  // than an overflow entry. Proxy-only sites have no process servlo can bounce.
  const showDevServerRestart = $derived(Boolean(site.host_has_dev_server) && !site.paused);

  const useTLS = $derived(Boolean(site.tls));
  const scheme = $derived(useTLS ? 'https://' : 'http://');

  const remoteView = $derived(!$accessMode.localControl);

  function openTarget() {
    openSiteInBrowser(site);
  }

  async function togglePause() {
    pauseBusy = true;
    try {
      await (site.paused ? resumeSite(site.domain) : pauseSite(site.domain));
      await loadSites();
    } finally {
      pauseBusy = false;
    }
  }

  async function restart() {
    restartBusy = true;
    try {
      const res = await restartSite(site.domain);
      if (!res.ok) openErrorModal(m.sites_restartFailed({ error: res.error || '' }));
    } finally {
      restartBusy = false;
    }
  }

  function onDocClick(ev: MouseEvent) {
    if (!overflowOpen) return;
    if (overflowEl && !overflowEl.contains(ev.target as Node)) overflowOpen = false;
  }
  function onDocKey(ev: KeyboardEvent) {
    if (ev.key === 'Escape' && overflowOpen) {
      overflowOpen = false;
      ev.stopPropagation();
    }
  }
  onMount(() => {
    document.addEventListener('click', onDocClick, true);
    document.addEventListener('keydown', onDocKey);
  });
  onDestroy(() => {
    document.removeEventListener('click', onDocClick, true);
    document.removeEventListener('keydown', onDocKey);
  });
</script>

<div class="border-b border-gray-100 dark:border-servlo-border shrink-0 @container flex flex-col">
  <div class="p-3 flex items-center gap-3">
    <div
      class="group flex-1 min-w-0 flex items-center gap-2 h-8 pl-3 pr-2 rounded-full border bg-gray-50 dark:bg-white/[0.03] transition-colors {site.paused
        ? 'border-gray-200 dark:border-servlo-border opacity-70'
        : 'border-gray-200 dark:border-servlo-border hover:bg-white dark:hover:bg-white/[0.06] hover:border-gray-300 dark:hover:border-gray-600 focus-within:bg-white focus-within:border-servlo-red/40'}"
    >
      <TLSControl {site} toggleable={tlsToggleable} />

      {#if site.has_favicon}
        <img
          src={apiBase + '/api/sites/' + site.domain + '/favicon'}
          class="w-4 h-4 shrink-0 rounded-xs object-contain"
          loading="lazy"
          alt=""
        />
      {:else}
        <svg
          class="w-4 h-4 shrink-0 text-gray-400 dark:text-gray-500"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M21 12a9 9 0 11-18 0 9 9 0 0118 0zM3.6 9h16.8M3.6 15h16.8M12 3a17 17 0 010 18M12 3a17 17 0 000 18"
          />
        </svg>
      {/if}

      {#if urlEditable}
        <button
          type="button"
          onclick={() => openDomainModal(site)}
          use:tooltip={m.sites_manageDomains()}
          aria-label={m.sites_manageDomains()}
          class="flex items-center min-w-0 flex-1 font-mono cursor-text text-left pt-1.5"
        >
          <span class="text-sm text-gray-400 dark:text-gray-500 shrink-0 leading-none">{scheme}</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100 truncate leading-none">{activeDomain}</span>
        </button>
      {:else}
        <span title={scheme + activeDomain} class="flex items-baseline min-w-0 flex-1 font-mono pt-1.5">
          <span class="text-sm text-gray-400 dark:text-gray-500 shrink-0 leading-none">{scheme}</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100 truncate leading-none">{activeDomain}</span>
        </span>
      {/if}

      <DomainMorePill {site} />

      <span class="flex items-center gap-1.5 shrink-0">
        {#if activeFrameworkLabel}
          <span class="hidden @md:inline-flex"><Badge tone="framework">{activeFrameworkLabel}</Badge></span>
        {/if}
        {#if site.paused}
          <span class="inline-flex items-center gap-1 text-[11px] text-amber-600 dark:text-amber-400 font-medium">
            <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 5h4v14H6zM14 5h4v14h-4z" />
            </svg>
            {m.sites_paused().toLowerCase()}
          </span>
        {/if}
      </span>

      {#if !site.paused}
        <button
          type="button"
          onclick={onOpenNginx}
          aria-label={m.sites_nginx_editTitle()}
          use:tooltip={m.sites_nginx_editTitle()}
          class="shrink-0 -mr-1 p-1 rounded-sm text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
            <line x1="3" y1="8" x2="21" y2="8" />
            <line x1="3" y1="16" x2="21" y2="16" />
            <line x1="9" y1="6" x2="9" y2="10" />
            <line x1="15" y1="14" x2="15" y2="18" />
          </svg>
        </button>
      {/if}
    </div>

    <div class="flex items-center shrink-0">

      {#if showDevServerRestart}
        <button
          type="button"
          onclick={restart}
          disabled={restartBusy}
          aria-label={m.sites_restartDevServer()}
          use:tooltip={m.sites_restartDevServer()}
          class="w-8 h-8 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-servlo-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors disabled:opacity-50"
        >
          <Icon name={restartBusy ? 'spinner' : 'refresh'} class="w-4 h-4 {restartBusy ? 'animate-spin' : ''}" />
        </button>
      {/if}

      {#if !site.host_proxy}
        <button
          type="button"
          onclick={() => openGroupModal(site)}
          aria-label={m.group_manage()}
          use:tooltip={site.group ? 'Manage group' : 'Group with another site'}
          class="w-8 h-8 flex items-center justify-center rounded-md transition-colors hover:bg-gray-100 dark:hover:bg-white/5 {site.group
            ? 'text-servlo-red'
            : 'text-gray-500 dark:text-gray-400 hover:text-servlo-red'}"
        >
          <Icon name="group" class="w-4 h-4" />
        </button>
      {/if}

      <!-- A group secondary shows its main's workspace and moves with it, so it
           has nothing of its own to pick. -->
      {#if $accessMode.localControl && !site.group_subdomain}
        <WorkspacePicker {site} />
      {/if}

      {#if $accessMode.localControl}
        <button
          type="button"
          onclick={() => openTerminal(site.domain)}
          aria-label={m.common_terminal()}
          use:tooltip={m.sites_openInTerminal()}
          class="hidden @md:flex w-8 h-8 items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-servlo-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
            />
          </svg>
        </button>
      {/if}

      <div class="relative" bind:this={overflowEl}>
        <button
          type="button"
          onclick={() => (overflowOpen = !overflowOpen)}
          aria-label={m.common_moreActions()}
          aria-haspopup="menu"
          aria-expanded={overflowOpen}
          use:tooltip={m.common_moreActions()}
          class="w-8 h-8 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-servlo-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 6a2 2 0 100-4 2 2 0 000 4zm0 8a2 2 0 100-4 2 2 0 000 4zm0 8a2 2 0 100-4 2 2 0 000 4z" />
          </svg>
        </button>
        {#if overflowOpen}
          <div
            role="menu"
            class="absolute right-0 top-full mt-1 min-w-[12rem] rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-bg shadow-lg z-30 py-1"
          >
            {#if !site.paused && !site.host_proxy && (site.uses_php || site.custom_container)}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  restart();
                }}
                disabled={restartBusy}
                class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                  />
                </svg>
                {restartBusy ? '...' : m.sites_restartContainer()}
              </button>
            {/if}
            {#if !site.paused}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  togglePin();
                }}
                disabled={pinBusy}
                class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50 {site.pinned
                  ? 'text-amber-600 dark:text-amber-400'
                  : 'text-gray-700 dark:text-gray-200'}"
              >
                <svg class="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill={site.pinned ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2" stroke-linejoin="round">
                  <path d="M16 9V4h1a1 1 0 0 0 0-2H7a1 1 0 0 0 0 2h1v5l-2 3v2h5v5l1 1 1-1v-5h5v-2l-2-3z" />
                </svg>
                {pinBusy ? '...' : site.pinned ? m.sites_unpin() : m.sites_pin()}
              </button>
            {/if}
            <button
              type="button"
              role="menuitem"
              onclick={() => {
                overflowOpen = false;
                togglePause();
              }}
              disabled={pauseBusy}
              class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50 {site.paused
                ? 'text-emerald-600 dark:text-emerald-400'
                : 'text-amber-600 dark:text-amber-400'}"
            >
              {#if site.paused}
                <svg class="w-3.5 h-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24"><path d="M6 4.5v15l13-7.5z" /></svg>
              {:else}
                <svg class="w-3.5 h-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24"><path d="M6.5 4.5h4v15h-4zM13.5 4.5h4v15h-4z" /></svg>
              {/if}
              {pauseBusy ? '...' : site.paused ? m.sites_resume() : m.sites_pause()}
            </button>
            {#if $accessMode.localControl}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  openTerminal(site.domain);
                }}
                class="@md:hidden w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
                  />
                </svg>
                {m.common_terminal()}
              </button>
            {/if}
            {#if !site.paused}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  openDomainModal(site);
                }}
                class="@md:hidden w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"
                  />
                </svg>
                {m.sites_manageDomains()}
              </button>
            {/if}
            <div class="my-1 border-t border-gray-100 dark:border-servlo-border"></div>
            <button
              type="button"
              role="menuitem"
              onclick={() => {
                overflowOpen = false;
                openSiteUnlinkModal({ domain: site.domain });
              }}
              class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
            >
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                />
              </svg>
              {m.sites_unlink()}
            </button>
          </div>
        {/if}
      </div>
    </div>
  </div>

  {#snippet pathLabel()}
    {#if $accessMode.localControl}
      <button
        type="button"
        onclick={() => openFolder(activePath)}
        use:tooltip={m.sites_openFolder()}
        class="font-mono leading-none truncate hover:text-servlo-red transition-colors"
      >{activePathLabel}</button>
    {:else}
      <span class="font-mono leading-none truncate" title={activePath}>{activePathLabel}</span>
    {/if}
  {/snippet}

  {#if activePath && !tabs}
    <div class="px-3 pb-2 flex items-center text-[11px] text-gray-500 dark:text-gray-400 min-w-0">
      {@render pathLabel()}
    </div>
  {/if}

  {#if tabs}
    <div class="px-3 flex items-end justify-between gap-4 -mb-px pt-1">
      <div class="flex items-end gap-4 min-w-0 overflow-x-auto">{@render tabs()}</div>
      {#if activePath}
        <div class="self-center min-w-0 max-w-[50%] flex items-center text-[11px] leading-none text-gray-500 dark:text-gray-400">
          {@render pathLabel()}
        </div>
      {/if}
    </div>
  {/if}
</div>
