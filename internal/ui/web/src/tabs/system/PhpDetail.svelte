<script lang="ts">
  import ButtonMenu, { type ButtonMenuAction } from '$components/ButtonMenu.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import PhpIniTab from './PhpIniTab.svelte';
  import PhpPortsTab from './PhpPortsTab.svelte';
  import PhpExtensionsTab from './PhpExtensionsTab.svelte';
  import { status, loadStatus } from '$stores/status';
  import { setDefaultPhp, startPhp, stopPhp, checkPhpUpdates } from '$stores/phpVersions';
  import { sites, sitesByPhp } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { openPhpRemoveModal, openPhpRebuildModal } from '$stores/modals';
  import { notifyLocalInfo } from '$lib/notify';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    version: string;
  }
  let { version }: Props = $props();

  const isDefault = $derived($status.php_default === version);
  const siteCount = $derived($sitesByPhp.get(version) ?? 0);
  const fpm = $derived($status.php_fpms.find((f) => f.version === version));
  const running = $derived(Boolean(fpm?.running));
  const container = $derived('servlo-php' + version.replace('.', '') + '-fpm');
  const sitesUsing = $derived($sites.filter((s) => s.php_version === version));
  const baseUpdate = $derived(Boolean(fpm?.update_available));

  let defaultBusy = $state(false);
  let fpmBusy = $state(false);
  let checking = $state(false);

  type TabId = 'logs' | 'sites' | 'config' | 'ports' | 'extensions';
  let active = $state<TabId>('logs');
  const tabs = $derived<TabItem<TabId>[]>([
    { id: 'logs', label: m.services_tabs_logs(), hidden: !running },
    { id: 'sites', label: m.system_php_sites() },
    { id: 'config', label: m.system_php_iniTab() },
    { id: 'ports', label: m.system_php_portsTab() },
    { id: 'extensions', label: m.system_php_extensionsTab() }
  ]);

  $effect(() => {
    if (active === 'logs' && !running) active = 'sites';
  });

  async function onSetDefault() {
    defaultBusy = true;
    try {
      await setDefaultPhp(version);
      await loadStatus();
    } finally {
      defaultBusy = false;
    }
  }

  async function onToggleFpm() {
    fpmBusy = true;
    try {
      await (running ? stopPhp(version) : startPhp(version));
      await loadStatus();
    } finally {
      fpmBusy = false;
    }
  }

  // Reads through to the registry, so it answers even when the cached digest is
  // hours old. The result goes to the toast surface rather than a line in the
  // action row, which would either push the controls out of line or sit on top
  // of whatever the tab below is showing. The badge follows from the status.
  async function runCheckUpdates() {
    checking = true;
    try {
      const res = await checkPhpUpdates(version);
      const label = 'PHP ' + version;
      if (!res.ok) {
        notifyLocalInfo('update_check', label, m.system_php_checkUpdatesFailed());
        return;
      }
      notifyLocalInfo(
        'update_check',
        label,
        res.status?.stale ? m.system_php_checkUpdatesFound() : m.system_php_checkUpdatesUpToDate()
      );
      await loadStatus();
    } finally {
      checking = false;
    }
  }

  const versionBusy = $derived(fpmBusy || defaultBusy || checking);

  const rebuildAction = $derived<ButtonMenuAction>({
    id: 'rebuild',
    tone: baseUpdate ? 'success' : undefined,
    icon: rebuildIcon,
    label: baseUpdate ? m.system_php_rebuildUpdate() : m.system_php_rebuild(),
    title: baseUpdate ? m.system_php_baseUpdateHint() : m.system_php_rebuildHint(),
    onclick: () => openPhpRebuildModal(version)
  });

  const versionActions = $derived.by<ButtonMenuAction[]>(() => {
    const acts: ButtonMenuAction[] = [];
    // Rebuild is only worth a button of its own when the base has actually
    // moved, the way an available service update is; with nothing to pick up it
    // stays in the menu rather than sitting there inviting a five-minute build.
    if (baseUpdate) {
      acts.push(rebuildAction);
    }
    const tail: ButtonMenuAction[] = [
      {
        id: 'check-updates',
        icon: checkUpdatesIcon,
        label: checking ? m.services_checkUpdatesChecking() : m.system_php_checkUpdates(),
        title: m.system_php_checkUpdatesTitle(),
        disabled: checking,
        onclick: runCheckUpdates
      }
    ];
    if (!baseUpdate) {
      tail.push(rebuildAction);
    }
    if (isDefault) {
      return [...acts, ...tail];
    }
    if (running) {
      acts.push({
        id: 'stop',
        icon: stopIcon,
        label: m.common_stop(),
        title: siteCount > 0 ? m.system_php_stopWarn({ count: siteCount }) : m.system_php_stopTitle(),
        disabled: fpmBusy,
        onclick: onToggleFpm
      });
    } else {
      acts.push({
        id: 'start',
        tone: 'success',
        icon: startIcon,
        label: m.common_start(),
        title: m.system_php_startTitle(),
        disabled: fpmBusy,
        onclick: onToggleFpm
      });
    }
    acts.push({
      id: 'set-default',
      icon: starIcon,
      label: m.system_php_setDefault(),
      disabled: defaultBusy,
      onclick: onSetDefault
    });
    acts.push({
      id: 'remove',
      tone: 'danger',
      icon: trashIcon,
      label: m.common_remove(),
      title: siteCount > 0 ? m.system_php_removeWarn({ count: siteCount }) : m.system_php_removeTitle(),
      onclick: () => openPhpRemoveModal({ version, siteCount })
    });
    return [...acts, ...tail];
  });
</script>

{#snippet startIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
{/snippet}
{#snippet stopIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
{/snippet}
{#snippet starIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 20 20"><path d="M10 1.5l2.6 5.27 5.82.85-4.21 4.1.99 5.78L10 14.77l-5.2 2.73.99-5.78L1.58 7.62l5.82-.85L10 1.5z"/></svg>
{/snippet}
{#snippet trashIcon()}
  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
{/snippet}
{#snippet rebuildIcon()}
  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
{/snippet}
{#snippet checkUpdatesIcon()}
  <svg class={`w-3.5 h-3.5 ${checking ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7"/><polyline points="21 4 21 10 15 10"/></svg>
{/snippet}

{#snippet detailActions()}
  <ButtonMenu actions={versionActions} busy={versionBusy} />
{/snippet}

<DetailTabs {tabs} {active} onchange={(id) => (active = id)} actions={detailActions} />
{#if active === 'logs' && running}
  <LogViewer path={'/api/logs/' + container} />
{:else if active === 'sites'}
  <div class="px-3 sm:px-5 py-3 shrink-0">
    {#if sitesUsing.length === 0}
      <p class="text-sm text-gray-400">{m.system_noSitesUsingPhp({ version })}</p>
    {:else}
      <div class="flex flex-wrap gap-2">
        {#each sitesUsing as s (s.domain)}
          <button
            onclick={() => goToTab('sites', s.domain)}
            class="inline-flex items-center gap-1.5 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-servlo-border text-gray-700 dark:text-gray-300 rounded-full px-2.5 py-1 transition-colors"
          >
            <span class="w-1.5 h-1.5 rounded-full shrink-0 {s.fpm_running ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
            {s.domain}
          </button>
        {/each}
      </div>
    {/if}
  </div>
{:else if active === 'config'}
  <PhpIniTab {version} />
{:else if active === 'ports'}
  <PhpPortsTab {version} />
{:else if active === 'extensions'}
  <PhpExtensionsTab {version} />
{/if}
