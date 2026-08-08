<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import Badge from '$components/Badge.svelte';
  import Icon from '$components/Icon.svelte';
  import {
    sites,
    sitesLoaded,
    siteWorkerFailing,
    openSiteInBrowser,
    type Site
  } from '$stores/sites';
  import { openAddSiteModal } from '$stores/modals';
  import { goToTab } from '$stores/route';
  import { isAdmin } from '$stores/session';
  import { apiBase } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  const total = $derived($sites.length);
  const running = $derived($sites.filter((s) => s.fpm_running && !s.paused).length);
  const paused = $derived($sites.filter((s) => s.paused).length);
  const failing = $derived($sites.filter((s) => siteWorkerFailing(s)).length);

  // The backend serialises sites.yaml in registry order — AddSite appends
  // and RemoveSite preserves the rest's positions, so the position of a
  // site in the array reflects when it was registered (oldest first).
  // Reverse and drop paused sites so the dashboard shows the most recently
  // added active projects at the top — paused sites are still visible on
  // the Sites tab.
  const sorted = $derived($sites.filter((s) => !s.paused).reverse());

  function onOpen(s: Site, evt: Event) {
    evt.stopPropagation();
    openSiteInBrowser(s);
  }
</script>

<DashboardCard title={m.dashboard_sites_title()} tone={failing > 0 ? 'critical' : 'default'}>
  {#snippet badge()}
    {#if $sitesLoaded}
      <StatusPill
        tone={failing > 0 ? 'error' : running > 0 ? 'ok' : 'muted'}
        label={m.dashboard_sites_summary({ running, total })}
      />
    {/if}
  {/snippet}

  {#if $sitesLoaded && total === 0}
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {@html m.sites_emptyHint({ cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm font-mono">servlo park</code>' })}
    </p>
  {:else}
    <div class="space-y-0.5">
      {#each sorted as s (s.domain)}
        <button
          onclick={() => goToTab('sites', s.domain)}
          class="group w-full flex items-center gap-2 px-1.5 py-1.5 rounded-md text-left hover:bg-gray-50 dark:hover:bg-white/4 transition-colors"
        >
          <span class="relative shrink-0 w-4 h-4 flex items-center justify-center">
            {#if s.has_favicon}
              <img src={apiBase + '/api/sites/' + s.domain + '/favicon'} class="w-4 h-4 rounded-xs object-contain" loading="lazy" alt="" />
            {:else}
              <StatusDot color={s.paused ? 'amber' : s.fpm_running ? 'green' : 'gray'} />
            {/if}
          </span>
          <span class="flex-1 min-w-0 text-sm font-medium text-gray-700 dark:text-gray-200 truncate">{s.domain}</span>
          {#if s.framework_label}
            <Badge tone="framework">{s.framework_label}</Badge>
          {/if}
          {#if siteWorkerFailing(s)}
            <span title={m.sites_workerFailing()} class="shrink-0"><StatusDot color="red" size="xs" pulse /></span>
          {/if}
          <span
            role="button"
            tabindex="0"
            title={m.dashboard_sites_openInBrowser()}
            onclick={(e) => onOpen(s, e)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') onOpen(s, e); }}
            class="shrink-0 w-7 h-7 inline-flex items-center justify-center rounded-sm text-gray-400 hover:text-servlo-red hover:bg-gray-100 dark:hover:bg-white/10 transition-colors cursor-pointer"
          >
            <Icon name="globe" class="w-3.5 h-3.5" />
          </span>
        </button>
      {/each}
    </div>
  {/if}

  {#snippet footer()}
    <div class="flex flex-wrap items-center gap-2">
      {#if $isAdmin}
        <button
          onclick={openAddSiteModal}
          class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white transition-colors"
        >
          <Icon name="plus" class="w-3.5 h-3.5" />
          {m.dashboard_sites_link()}
        </button>
      {/if}
      <button
        onclick={() => goToTab('sites')}
        class="ml-auto text-xs font-medium text-servlo-red hover:text-servlo-redhov"
      >{m.dashboard_sites_open()}</button>
    </div>
  {/snippet}
</DashboardCard>
