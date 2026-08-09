<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import { alerts, criticalAlerts, dismissAlert } from '$stores/alerts';
  import { m } from '../../paraglide/messages.js';

  // What is wrong, worst first: a site nobody can reach outranks a disk that
  // will be full next week. Anything the server raises that this list does not
  // know sorts last rather than first, which is what indexOf's -1 would do.
  const order = [
    'site_down',
    'cert_renew_failed',
    'backup_failed',
    'backup_unverified',
    'worker_down',
    'deploy_failed',
    'disk_filling'
  ];
  const rank = (kind: string) => (order.indexOf(kind) + order.length + 1) % (order.length + 1);
  const sorted = $derived(
    [...$alerts].sort((a, b) => rank(a.kind) - rank(b.kind) || (a.at < b.at ? 1 : -1))
  );

  let busy = $state('');
  let error = $state('');

  const idOf = (kind: string, site?: string) => kind + ' ' + (site ?? '');

  async function dismiss(kind: string, site?: string) {
    busy = idOf(kind, site);
    error = (await dismissAlert(kind, site)) ?? '';
    busy = '';
  }

  // One line of the message, with the blank-line-separated tail left for the
  // tooltip. Six alerts four lines deep is a card nobody reads.
  const firstLine = (message: string) => message.trim().split('\n')[0];

  function when(at: string): string {
    const mins = Math.max(0, Math.round((Date.now() - new Date(at).getTime()) / 60000));
    if (mins < 60) return m.alerts_sinceMinutes({ count: mins });
    if (mins < 60 * 48) return m.alerts_sinceHours({ count: Math.round(mins / 60) });
    return m.alerts_sinceDays({ count: Math.round(mins / 1440) });
  }
</script>

<DashboardCard title={m.alerts_title()} tone={$criticalAlerts.length > 0 ? 'critical' : 'warn'}>
  {#snippet badge()}
    <StatusPill
      tone={$criticalAlerts.length > 0 ? 'error' : 'warn'}
      label={m.alerts_count({ count: $alerts.length })}
    />
  {/snippet}

  {#if error}
    <p class="text-xs text-red-600 dark:text-red-400">{error}</p>
  {/if}

  {#each sorted as alert, i (idOf(alert.kind, alert.site))}
    <div
      class="flex items-start justify-between gap-2 {i > 0
        ? 'pt-2.5 border-t border-gray-100 dark:border-servlo-border'
        : ''}"
    >
      <div class="min-w-0">
        <!-- What and where on the first line, why and when on the second. The
             title never truncates: it is the one part that has to be readable
             at a glance, and a card this narrow will cut whichever field is
             allowed to give. Everything cut is in the tooltip. -->
        <div class="flex items-baseline gap-2 min-w-0">
          <span class="text-sm font-medium text-gray-800 dark:text-gray-100 shrink-0">{alert.title}</span>
          <span class="text-xs text-gray-500 dark:text-gray-400 truncate" title={alert.site}>
            {alert.site || m.alerts_thisServer()}
          </span>
        </div>
        <div class="flex items-baseline gap-2 min-w-0">
          <span class="text-xs text-gray-500 dark:text-gray-400 truncate" title={alert.message}>
            {firstLine(alert.message)}
          </span>
          <span class="text-xs text-gray-400 dark:text-gray-500 shrink-0">{when(alert.at)}</span>
        </div>
      </div>
      <button
        type="button"
        onclick={() => dismiss(alert.kind, alert.site)}
        disabled={busy === idOf(alert.kind, alert.site)}
        title={m.alerts_dismissHint()}
        class="shrink-0 inline-flex items-center px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border text-xs font-medium text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-white/5 dark:hover:text-white disabled:opacity-50 transition-colors"
      >
        {m.alerts_dismiss()}
      </button>
    </div>
  {/each}
</DashboardCard>
