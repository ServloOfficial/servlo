<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import SiteHeader from './SiteHeader.svelte';
  import SiteControls from './SiteControls.svelte';
  import SiteLogs from './SiteLogs.svelte';
  import WorktreeList from './WorktreeList.svelte';
  import SiteTinkerTab from './SiteTinkerTab.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  type TabId = 'overview' | 'tinker';
  const TAB_STORAGE_KEY = 'lerd:siteDetailTab';

  function readStoredTab(): TabId {
    if (typeof localStorage === 'undefined') return 'overview';
    const v = localStorage.getItem(TAB_STORAGE_KEY);
    return v === 'tinker' ? 'tinker' : 'overview';
  }

  let active = $state<TabId>(readStoredTab());
  const canTinker = $derived(Boolean(site.php_version));

  $effect(() => {
    if (active === 'tinker' && !canTinker) active = 'overview';
  });

  $effect(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(TAB_STORAGE_KEY, active);
    }
  });

  const tabBtn = (tab: TabId, isActive: boolean) =>
    'pb-1 text-xs font-medium border-b-2 transition-colors ' +
    (isActive
      ? 'border-lerd-red text-lerd-red'
      : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300');
</script>

{#snippet tabs()}
  <button class={tabBtn('overview', active === 'overview')} onclick={() => (active = 'overview')}>Overview</button>
  {#if canTinker}
    <button class={tabBtn('tinker', active === 'tinker')} onclick={() => (active = 'tinker')}>Tinker</button>
  {/if}
{/snippet}

<DetailPanel>
  <SiteHeader {site} {tabs} />
  {#if active === 'overview'}
    {#if !site.paused}
      <SiteControls {site} />
    {/if}
    <WorktreeList {site} />
    <SiteLogs {site} />
  {:else if active === 'tinker'}
    <SiteTinkerTab {site} />
  {/if}
</DetailPanel>
