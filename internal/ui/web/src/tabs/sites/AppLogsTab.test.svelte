<script lang="ts">
  import AppLogsTab from './AppLogsTab.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();
</script>

<AppLogsTab {site} />
