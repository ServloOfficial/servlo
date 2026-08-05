<script lang="ts">
  import Icon from '$components/Icon.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import { runningWorkerColors, siteWorkerFailing, type Site } from '$stores/sites';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const dots = $derived(runningWorkerColors(site));
</script>

{#if siteWorkerFailing(site)}
  <span title={m.sites_workerFailing()} class="shrink-0"><StatusDot color="red" size="xs" pulse /></span>
{/if}
{#each dots as c, i (i + ':' + c)}
  <StatusDot color={c} size="xs" />
{/each}
