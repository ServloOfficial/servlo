<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import Icon from '$components/Icon.svelte';
  import ListRow from '$components/ListRow.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { routeRest, goToTab } from '$stores/route';
  import { status, statusLoaded, servloStatusColor, allCoreRunning } from '$stores/status';
  import { phpVersions } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { sitesByNode } from '$stores/sites';
  import { version } from '$stores/version';
  import { isAdmin } from '$stores/session';
  import { servloStart, servloStop, servloStarting, servloStopping } from '$stores/servloLifecycle';
  import { notifyPrefs, permissionState, autoSubscribeDisabled } from '$lib/notify';
  import { onMount } from 'svelte';
  import { m } from '../paraglide/messages.js';

  onMount(() => {
  });

  const selected = $derived($routeRest || 'servlo');
  // Delivery needs a granted permission and an active subscription.
  const notifyEffectiveOn = $derived(
    $permissionState === 'granted' && !$autoSubscribeDisabled && $notifyPrefs.enabled
  );

  function select(id: string) {
    goToTab('system', id);
  }
</script>

{#snippet actions()}
  {#if $isAdmin && !$allCoreRunning}
    <ActionButton
      title={m.system_startServlo()}
      tone="success"
      onclick={servloStart}
      disabled={$servloStarting || $servloStopping}
      loading={$servloStarting}
    >
      <Icon name="play" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
  {#if $isAdmin}
    <ActionButton
      title={m.system_stopServlo()}
      onclick={servloStop}
      disabled={$servloStarting || $servloStopping}
      loading={$servloStopping}
    >
      <Icon name="stop" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
{/snippet}

<ListPanel title={m.system_title()} {actions}>
  {#if !$statusLoaded}
    <LoadingRow />
  {:else}
    {#snippet nginxDot()}<StatusDot color={$status.nginx.running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'nginx'} onclick={() => select('nginx')} leading={nginxDot}>{m.system_nginx()}</ListRow>

    {#if $phpVersions.length > 0}
      {@const phpSelected = selected === 'php' || selected.startsWith('php-')}
      {@const anyFpmRunning = $status.php_fpms.some((f) => f.running)}
      {#snippet phpLeading()}<StatusDot color={anyFpmRunning ? 'green' : 'gray'} />{/snippet}
      {#snippet phpTrailing()}
        <span class="text-[10px] font-medium tabular-nums shrink-0 {phpSelected ? 'text-servlo-red/70' : 'text-gray-400 dark:text-gray-600'}">{$phpVersions.length}</span>
      {/snippet}
      <ListRow active={phpSelected} onclick={() => select('php')} leading={phpLeading} trailing={phpTrailing}>PHP</ListRow>
    {/if}

    {#snippet nodeLeading()}<StatusDot color={$status.using_system_bun ? 'amber' : $status.node_managed_by_servlo ? 'green' : 'blue'} />{/snippet}
    {#snippet nodeTrailing()}
      {#if $status.using_system_bun}
        <span class="text-[10px] font-medium shrink-0 text-amber-600 dark:text-amber-400">🥟 {$status.bun_version}</span>
      {:else}
        <span class="text-[10px] font-medium tabular-nums shrink-0 {selected === 'node' ? 'text-servlo-red/70' : 'text-gray-400 dark:text-gray-600'}">{$nodeVersions.length}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'node'} onclick={() => select('node')} leading={nodeLeading} trailing={nodeTrailing}>
      {$status.using_system_bun ? m.dashboard_health_jsRuntime() : m.system_nodeJs()}
    </ListRow>

    {@const toolsAttention = ($status.tools ?? []).some((t) => t.update_available || !t.present)}
    {#snippet toolsDot()}<StatusDot color={toolsAttention ? 'amber' : 'green'} />{/snippet}
    {#snippet toolsTrailing()}
      {#if ($status.tools ?? []).some((t) => t.update_available)}
        <span class="ml-auto text-xs font-medium text-yellow-600 dark:text-yellow-400">{m.system_servlo_updateTag()}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'tools'} onclick={() => select('tools')} leading={toolsDot} trailing={toolsTrailing}>
      {m.system_tools_title()}
    </ListRow>

    {#snippet notifyDot()}<StatusDot color={notifyEffectiveOn ? 'green' : 'red'} />{/snippet}
    <ListRow active={selected === 'notifications'} onclick={() => select('notifications')} leading={notifyDot}>
      {m.notify_settings_title()}
    </ListRow>


    {#if $isAdmin}
      {#snippet sftpDot()}<StatusDot color="gray" />{/snippet}
      <ListRow active={selected === 'sftp'} onclick={() => select('sftp')} leading={sftpDot}>
        {m.system_sftp()}
      </ListRow>

      {#snippet mailDot()}<StatusDot color="gray" />{/snippet}
      <ListRow active={selected === 'mail'} onclick={() => select('mail')} leading={mailDot}>
        {m.system_mail()}
      </ListRow>

      {#snippet securityDot()}<StatusDot color="gray" />{/snippet}
      <ListRow active={selected === 'security'} onclick={() => select('security')} leading={securityDot}>
        {m.system_security()}
      </ListRow>
    {/if}

    {#snippet watcherDot()}<StatusDot color={$status.watcher_running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'watcher'} onclick={() => select('watcher')} leading={watcherDot}>{m.system_watcher()}</ListRow>

    {#snippet servloLeading()}<StatusDot color={$servloStatusColor} />{/snippet}
    {#snippet servloTrailing()}
      {#if $version.hasUpdate}
        <span class="ml-auto text-xs font-medium text-yellow-600 dark:text-yellow-400">{m.system_servlo_updateTag()}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'servlo'} onclick={() => select('servlo')} leading={servloLeading} trailing={servloTrailing}>
      {m.system_servlo()}
    </ListRow>
  {/if}
</ListPanel>
