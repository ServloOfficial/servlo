<script lang="ts">
  import WorktreeDBIsolateModal from './WorktreeDBIsolateModal.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    open: boolean;
    site: Site;
    branch: string;
    onclose: () => void;
    onconfirm: (source: string) => void;
  }
  let { open, site, branch, onclose, onconfirm }: Props = $props();
</script>

<WorktreeDBIsolateModal {open} {site} {branch} {onclose} {onconfirm} />
