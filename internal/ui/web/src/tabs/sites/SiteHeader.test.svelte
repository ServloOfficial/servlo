<script lang="ts">
  import SiteHeader from './SiteHeader.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
    withTabs?: boolean;
  }
  let { site, withTabs = true }: Props = $props();
</script>

{#snippet tabs()}
  <button type="button">Overview</button>
  <button type="button">Env</button>
{/snippet}

<SiteHeader {site} tabs={withTabs ? tabs : undefined} />
