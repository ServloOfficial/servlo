<script lang="ts">
  import WorktreePicker from './WorktreePicker.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
    activeBranch: string;
    onchange: (branch: string) => void;
  }
  let { site, activeBranch, onchange }: Props = $props();
</script>

<WorktreePicker {site} {activeBranch} {onchange} />
