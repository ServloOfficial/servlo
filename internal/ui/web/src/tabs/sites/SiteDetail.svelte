<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import SiteHeader from './SiteHeader.svelte';
  import SiteOverview from './SiteOverview.svelte';
  import SiteLogs from './SiteLogs.svelte';
  import SiteEnvTab from './SiteEnvTab.svelte';
  import SitePHPSettingsTab from './SitePHPSettingsTab.svelte';
  import SiteDeployTab from './SiteDeployTab.svelte';
  import SiteFilesTab from './SiteFilesTab.svelte';
  import SiteCronTab from './SiteCronTab.svelte';
  import SiteNginxModal from '../../modals/SiteNginxModal.svelte';
  import { resumeSite, loadSites, siteHasLogSources, type Site } from '$stores/sites';
  import { routeRest, goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';

  let resumeBusy = $state(false);
  async function onResume() {
    resumeBusy = true;
    try {
      await resumeSite(site.domain);
      await loadSites();
    } finally {
      resumeBusy = false;
    }
  }

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  type TabId = 'overview' | 'logs' | 'env' | 'deploy' | 'cron' | 'files' | 'settings';
  const TAB_STORAGE_KEY = 'servlo:siteDetailTab';

  function readStoredTab(): TabId {
    if (typeof localStorage === 'undefined') return 'overview';
    const v = localStorage.getItem(TAB_STORAGE_KEY);
    if (v === 'logs' || v === 'env' || v === 'deploy' || v === 'cron' || v === 'files' || v === 'settings')
      return v;
    return 'overview';
  }

  let active = $state<TabId>(readStoredTab());
  let nginxOpen = $state(false);
  const canEnv = $derived(Boolean(site.has_env));
  // Logs get their own tab in the resource layout rather than living under the
  // overview. Offer it whenever the site exposes any log source, including a
  // worker-only source on a proxy-only host site.
  const canLogs = $derived(siteHasLogSources(site));
  // A lone Overview tab can't be switched to anything, so don't render the tab
  // row at all when no other tab is available (e.g. static sites).
  // Settings is offered for anything served by PHP-FPM: it writes a pool, and
  // a site with no pool has nothing for these fields to change.
  const canSettings = $derived(
    !site.custom_container && site.runtime !== 'frankenphp' && !site.host_port
  );
  // Deploy is offered to any site with a repository: the tab is git pull plus a
  // script, and a site that was never cloned has nothing to pull.
  const canDeploy = $derived(Boolean(site.branch));
  // A scheduled command runs inside the site's container, so a host-proxy site
  // has nowhere to run one and is not offered the tab at all.
  const canCron = $derived(!site.host_port);
  // The file manager browses the site's own directory, which every site has.
  const canFiles = $derived(true);
  const hasExtraTabs = $derived(canLogs || canEnv || canDeploy || canCron || canFiles || canSettings);

  // The route can deep-link a sub-tab. When the second segment names a tab, honour it
  // and overwrite the stored selection.
  $effect(() => {
    const seg = $routeRest.split('/')[1] ?? '';
    if (seg === 'nginx') {
      nginxOpen = true;
    } else if (
      seg === 'logs' ||
      seg === 'env' ||
      seg === 'deploy' ||
      seg === 'cron' ||
      seg === 'files' ||
      seg === 'settings' ||
      seg === 'overview'
    ) {
      active = seg;
    }
  });

  $effect(() => {
    if (active === 'logs' && !canLogs) active = 'overview';
    if (active === 'env' && !canEnv) active = 'overview';
    if (active === 'deploy' && !canDeploy) active = 'overview';
    if (active === 'files' && !canFiles) active = 'overview';
    if (active === 'cron' && !canCron) active = 'overview';
    if (active === 'settings' && !canSettings) active = 'overview';
  });

  $effect(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(TAB_STORAGE_KEY, active);
    }
  });

  // Select a tab and mirror it into the URL hash so the two never drift. Without
  // this, clicking a tab left the hash pointing at whatever deep link last set it
  // (e.g. the doctor's "edit env" #sites/<d>/env), so a refresh snapped back to
  // that tab and a repeat deep link to the same hash fired no hashchange and so
  // appeared to do nothing.
  function selectTab(t: TabId) {
    active = t;
    goToTab('sites', `${site.domain}/${t}`);
  }

  const tabBtn = (tab: TabId, isActive: boolean) =>
    'pb-1 text-xs font-medium border-b-2 transition-colors ' +
    (isActive
      ? 'border-servlo-red text-servlo-red'
      : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300');
</script>

{#snippet tabs()}
  <button class={tabBtn('overview', active === 'overview')} onclick={() => selectTab('overview')}>{m.sites_tabs_overview()}</button>
  {#if canLogs}
    <button class={tabBtn('logs', active === 'logs')} onclick={() => selectTab('logs')}>{m.services_tabs_logs()}</button>
  {/if}
  {#if canEnv}
    <button class={tabBtn('env', active === 'env')} onclick={() => selectTab('env')}>{m.sites_tabs_env()}</button>
  {/if}
  {#if canDeploy}
    <button class={tabBtn('deploy', active === 'deploy')} onclick={() => selectTab('deploy')}>{m.sites_tabs_deploy()}</button>
  {/if}
  {#if canCron}
    <button class={tabBtn('cron', active === 'cron')} onclick={() => selectTab('cron')}>{m.sites_tabs_cron()}</button>
  {/if}
  {#if canFiles}
    <button class={tabBtn('files', active === 'files')} onclick={() => selectTab('files')}>{m.sites_tabs_files()}</button>
  {/if}
  {#if canSettings}
    <button class={tabBtn('settings', active === 'settings')} onclick={() => selectTab('settings')}>{m.sites_tabs_settings()}</button>
  {/if}
{/snippet}

<DetailPanel>
  <SiteHeader
    {site}
    tabs={site.paused || !hasExtraTabs ? undefined : tabs}
    onOpenNginx={() => (nginxOpen = true)}
  />
  {#if site.paused}
    <div class="flex-1 flex items-center justify-center px-6">
      <div class="flex flex-col items-center gap-3 max-w-md text-center">
        <svg class="w-10 h-10 text-gray-400 dark:text-gray-600" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="6" y="5" width="4" height="14" rx="1" />
          <rect x="14" y="5" width="4" height="14" rx="1" />
        </svg>
        <h2 class="text-base font-semibold text-gray-700 dark:text-gray-200">{m.sites_pausedDetail_title()}</h2>
        <p class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
          {m.sites_pausedDetail_hint({ domain: site.domain })}
        </p>
        <button
          type="button"
          onclick={onResume}
          disabled={resumeBusy}
          class="mt-1 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
        >
          {resumeBusy ? m.sites_pausedDetail_busy() : m.sites_pausedDetail_action()}
        </button>
      </div>
    </div>
  {:else if active === 'overview'}
    <SiteOverview {site} />
  {:else if active === 'logs'}
    <SiteLogs {site} />
  {:else if active === 'env'}
    {#key site.domain}
      <SiteEnvTab {site} />
    {/key}
  {:else if active === 'deploy'}
    {#key site.domain}
      <SiteDeployTab {site} />
    {/key}
  {:else if active === 'cron'}
    {#key site.domain}
      <SiteCronTab {site} />
    {/key}
  {:else if active === 'files'}
    {#key site.domain}
      <SiteFilesTab {site} />
    {/key}
  {:else if active === 'settings'}
    {#key site.domain}
      <SitePHPSettingsTab {site} onOpenRaw={() => (nginxOpen = true)} />
    {/key}
  {/if}
</DetailPanel>

<SiteNginxModal
  {site}
  domain={site.domain}
  open={nginxOpen}
  onclose={() => (nginxOpen = false)}
/>
