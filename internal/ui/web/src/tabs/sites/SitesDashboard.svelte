<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import Icon from '$components/Icon.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import DashboardHeader from '$components/DashboardHeader.svelte';
  import DashboardSection from '$components/DashboardSection.svelte';
  import SiteTile from './SiteTile.svelte';
  import { sites, sitesLoaded, siteWorkerFailing, type Site } from '$stores/sites';
  import { isAdmin } from '$stores/session';
  import { status } from '$stores/status';
  import { openAddSiteModal } from '$stores/modals';
  import { m } from '../../paraglide/messages.js';

  const total = $derived($sites.length);
  const active = $derived($sites.filter((s) => !s.paused));
  const paused = $derived($sites.filter((s) => s.paused));
  const running = $derived(active.filter((s) => s.fpm_running).length);
  const failing = $derived($sites.filter(siteWorkerFailing).length);

  const workspaceNames = $derived($status.workspaces ?? []);
  const hasWorkspaces = $derived(workspaceNames.length > 0);

  // Groups keep their first-seen order; unknown frameworks fold into a trailing
  // "Other" bucket. This is the overview's original grouping, kept for as long
  // as the user has no workspace to organise by.
  function byFramework(): Array<{ label: string; sites: Site[] }> {
    const order: string[] = [];
    const byLabel = new Map<string, Site[]>();
    for (const s of active) {
      const label = s.framework_label || m.sites_dash_otherFramework();
      if (!byLabel.has(label)) {
        byLabel.set(label, []);
        order.push(label);
      }
      byLabel.get(label)!.push(s);
    }
    const other = m.sites_dash_otherFramework();
    order.sort((a, b) => (a === other ? 1 : b === other ? -1 : 0));
    return order.map((label) => ({ label, sites: byLabel.get(label)! }));
  }

  // Active sites grouped by workspace, in the order the config lists them. An
  // empty workspace is hidden here — the sidebar is where those are managed —
  // and the framework stays visible as each tile's badge.
  function byWorkspace(): Array<{ label: string; sites: Site[] }> {
    const byName = new Map<string, Site[]>(workspaceNames.map((n) => [n, []]));
    for (const s of active) {
      if (s.workspace && byName.has(s.workspace)) byName.get(s.workspace)!.push(s);
    }
    return workspaceNames.filter((n) => byName.get(n)!.length > 0).map((label) => ({ label, sites: byName.get(label)! }));
  }

  const groups = $derived.by(() => (hasWorkspaces ? byWorkspace() : byFramework()));

  // Sites in no workspace trail the named ones under the same heading the
  // framework grouping already gives its leftovers.
  //
  // They used to trail with no heading at all, meant to mirror the sidebar. The
  // sidebar can do that because it sets them at a different indent; a grid of
  // identical tiles has no such cue, so the last workspace's heading ran on and
  // owned them. A site belonging to one client read as belonging to another,
  // which is the one thing a view organised by client must not do.
  const ungrouped = $derived(
    hasWorkspaces ? active.filter((s) => !s.workspace || !workspaceNames.includes(s.workspace)) : []
  );
</script>

<div class="flex-1 overflow-y-auto">
  <DashboardHeader title={m.sites_dash_overview()} stats={$sitesLoaded && total > 0 ? summary : undefined} />

  <div class="p-4 space-y-6">
    {#if !$sitesLoaded}
      <LoadingRow />
    {:else if total === 0}
      <EmptyState title={m.sites_empty()} hint={parkHint} />
    {:else}
      {#each groups as group (group.label)}
        <DashboardSection label={group.label}>
          {#each group.sites as site (site.domain)}
            <SiteTile {site} />
          {/each}
        </DashboardSection>
      {/each}

      {#if ungrouped.length > 0}
        <DashboardSection label={m.sites_dash_otherFramework()}>
          {#each ungrouped as site (site.domain)}
            <SiteTile {site} />
          {/each}
        </DashboardSection>
      {/if}

      {#if paused.length > 0}
        <DashboardSection label={m.sites_paused()}>
          {#each paused as site (site.domain)}
            <SiteTile {site} />
          {/each}
        </DashboardSection>
      {/if}
    {/if}

    {#if $isAdmin && $sitesLoaded && total > 0}
      <button
        onclick={openAddSiteModal}
        class="inline-flex items-center gap-1 text-xs font-medium text-servlo-red hover:text-servlo-redhov"
      >
        <Icon name="plus" class="w-3.5 h-3.5" />
        {m.sites_linkNew()}
      </button>
    {/if}
  </div>
</div>

{#snippet summary()}
  <span class="inline-flex items-center gap-1.5">
    <StatusDot color={running > 0 ? 'green' : 'gray'} />
    {m.dashboard_sites_summary({ running, total })}
  </span>
  {#if paused.length > 0}
    <span class="inline-flex items-center gap-1.5">
      <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24"><path d="M6 5h4v14H6zM14 5h4v14h-4z"/></svg>
      {m.dashboard_sites_pausedCount({ count: paused.length })}
    </span>
  {/if}
  {#if failing > 0}
    <span class="inline-flex items-center gap-1.5 text-red-500">
      <StatusDot color="red" size="xs" pulse />
      {m.dashboard_workers_failing({ count: failing })}
    </span>
  {/if}
{/snippet}

{#snippet parkHint()}
  {@html m.sites_emptyHint({ cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">servlo park</code>' })}
{/snippet}
