<script lang="ts">
  import { routeRest } from '$stores/route';
  import NginxDetail from './system/NginxDetail.svelte';
  import WatcherDetail from './system/WatcherDetail.svelte';
  import NotificationsDetail from './system/NotificationsDetail.svelte';
  import PhpPage from './system/PhpPage.svelte';
  import NodePage from './system/NodePage.svelte';
  import ServloDetail from './system/ServloDetail.svelte';
  import ToolsDetail from './system/ToolsDetail.svelte';
  import WorkerModeDetail from './system/WorkerModeDetail.svelte';
  import SftpDetail from './system/SftpDetail.svelte';
  import MailDetail from './system/MailDetail.svelte';
  import SecurityDetail from './system/SecurityDetail.svelte';

  const selected = $derived($routeRest || 'servlo');
  const phpVersion = $derived(selected.startsWith('php-') ? selected.slice(4) : '');
  const showPhp = $derived(selected === 'php' || selected.startsWith('php-'));
</script>

{#if selected === 'nginx'}
  <NginxDetail />
{:else if selected === 'watcher'}
  <WatcherDetail />
{:else if selected === 'notifications'}
  <NotificationsDetail />
{:else if showPhp}
  <PhpPage initialVersion={phpVersion} />
{:else if selected === 'node' || selected === 'node-install' || selected.startsWith('node-')}
  <NodePage />
{:else if selected === 'workermode'}
  <WorkerModeDetail />
{:else if selected === 'sftp'}
  <SftpDetail />
{:else if selected === 'mail'}
  <MailDetail />
{:else if selected === 'security'}
  <SecurityDetail />
{:else if selected === 'tools'}
  <ToolsDetail />
{:else}
  <ServloDetail />
{/if}
