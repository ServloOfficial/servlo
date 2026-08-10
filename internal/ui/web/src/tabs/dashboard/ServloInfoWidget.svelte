<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import CheckUpdatesButton from '$components/CheckUpdatesButton.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import ActivityList from './ActivityList.svelte';
  import { version, loadVersion } from '$stores/version';
  import { autostartEnabled } from '$stores/autostart';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';

  let changelogOpen = $state(false);
</script>

<DashboardCard title={m.dashboard_servlo_title()} tone={$version.hasUpdate ? 'warn' : 'default'}>
  {#snippet badge()}
    <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400 font-mono">
      v{$version.current}
    </span>
  {/snippet}

  {#if $version.checked && !$version.hasUpdate}
    <div class="flex items-center gap-2 text-sm text-emerald-600 dark:text-emerald-500">
      <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/>
      </svg>
      {m.system_servlo_latest()}
    </div>
  {/if}

  {#if $version.hasUpdate}
    <div class="flex items-start gap-2 text-sm text-yellow-700 dark:text-yellow-400 bg-yellow-50 dark:bg-yellow-500/10 border border-yellow-200 dark:border-yellow-500/30 rounded-lg px-3 py-2">
      <svg class="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>
      </svg>
      <div class="flex-1 space-y-2">
        <span>{m.system_servlo_available({ version: $version.latest })}</span>
        {#if $version.changelog}
          <button
            type="button"
            onclick={() => (changelogOpen = !changelogOpen)}
            class="inline-flex items-center gap-1 text-[11px] font-medium text-yellow-700/80 dark:text-yellow-300/80 hover:text-yellow-800 dark:hover:text-yellow-200 transition-colors"
            aria-expanded={changelogOpen}
          >
            <svg class="w-3 h-3 transition-transform {changelogOpen ? 'rotate-90' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/>
            </svg>
            {changelogOpen ? m.dashboard_servlo_hideChangelog() : m.dashboard_servlo_viewChangelog()}
          </button>
          {#if changelogOpen}
            <pre class="text-[11px] leading-relaxed font-mono text-yellow-900/90 dark:text-yellow-100/90 bg-yellow-100/40 dark:bg-yellow-500/10 border border-yellow-200/60 dark:border-yellow-500/20 rounded-md p-2 max-h-[180px] overflow-y-auto whitespace-pre-wrap">{$version.changelog}</pre>
          {/if}
        {/if}
      </div>
    </div>
  {/if}

  

  <div class="pt-2 border-t border-gray-100 dark:border-servlo-border space-y-1.5">
    <div class="text-[10px] font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wide">{m.dashboard_activity_title()}</div>
    <ActivityList />
  </div>

  {#snippet footer()}
    <div class="flex items-center gap-2">
      <CheckUpdatesButton onclick={() => loadVersion(true)} checking={$version.checking} size="sm" />
      <button
        onclick={() => goToTab('system', 'servlo')}
        class="ml-auto text-xs font-medium text-servlo-red hover:text-servlo-redhov"
      >{m.dashboard_servlo_manage()}</button>
    </div>
  {/snippet}
</DashboardCard>
