<script lang="ts">
  import AppLogsTab from './AppLogsTab.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
    branch: string;
  }
  let { site, branch }: Props = $props();
</script>

<AppLogsTab {site} {branch} />
