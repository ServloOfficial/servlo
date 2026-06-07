<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import Icon from '$components/Icon.svelte';
  import { goToTab } from '$stores/route';
  import { serviceLabel, type Service } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();
</script>

<button
  onclick={() => goToTab('services', svc.name)}
  class="flex items-center gap-2.5 w-full text-left rounded-lg border border-gray-100 dark:border-lerd-border bg-white dark:bg-lerd-card hover:border-gray-200 dark:hover:border-white/15 hover:bg-gray-50 dark:hover:bg-white/5 px-3 py-2 transition-colors"
>
  <StatusDot color={svc.status === 'active' ? 'green' : 'gray'} />
  <span class="flex-1 min-w-0 truncate text-sm font-medium text-gray-700 dark:text-gray-200">
    {serviceLabel(svc.name)}
  </span>
  {#if svc.update_available}
    <span
      class="shrink-0 text-[10px] font-medium text-emerald-600 dark:text-emerald-400"
      title={svc.latest_version ? m.services_updateAvailableTo({ tag: svc.latest_version }) : m.services_updateAvailable()}
    >↑</span>
  {/if}
  {#if svc.version}
    <span class="shrink-0 text-[10px] font-mono tabular-nums text-gray-400 dark:text-gray-500 truncate">{svc.version}</span>
  {/if}
  {#if svc.site_count > 0}
    <span class="shrink-0 inline-flex items-center gap-1 text-[10px] tabular-nums text-gray-400 dark:text-gray-500" title={m.common_sites()}>
      <Icon name="sites" class="w-3 h-3" />
      {svc.site_count}
    </span>
  {/if}
</button>
