<script lang="ts">
  import Badge from '$components/Badge.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import type { PillTone } from '$components/StatusPill.svelte';
  import type { CronEntry } from '$stores/cron';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    entry: CronEntry;
    onedit: () => void;
    ondelete: () => void;
  }
  let { entry, onedit, ondelete }: Props = $props();

  let showOutput = $state(false);

  // Short enough to sit in a meta line beside three other facts. Seconds are
  // noise on a schedule whose finest grain is a minute.
  function when(iso?: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit'
    });
  }

  // Four states, and the panel has to tell them apart: never run reads as a job
  // that quietly does nothing, and a failure that reads as "no runs yet" is the
  // one nobody investigates.
  const status = $derived.by((): { tone: PillTone; label: string } => {
    if (entry.disabled) return { tone: 'muted', label: m.sites_cron_notScheduled() };
    if (entry.last_run?.running) return { tone: 'warn', label: m.sites_cron_running() };
    if (!entry.last_run) return { tone: 'muted', label: m.sites_cron_neverRun() };
    if (entry.last_run.ok) return { tone: 'ok', label: m.sites_cron_lastRunOK() };
    return { tone: 'error', label: m.sites_cron_lastRunFailed() };
  });

  const output = $derived(entry.last_run?.output ?? []);

  const action =
    'px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border text-xs text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 disabled:opacity-40 disabled:hover:text-gray-600 transition-colors';
</script>

<div class="py-3 border-t border-gray-100 dark:border-servlo-border first:border-t-0 first:pt-0">
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0">
      <div class="flex items-center gap-2">
        <span class="text-xs font-medium text-gray-800 dark:text-gray-100 truncate">{entry.name}</span>
        {#if entry.managed}
          <Badge tone="neutral" title={m.sites_cron_managedHint()}>{m.sites_cron_managed()}</Badge>
        {/if}
      </div>
      <p class="mt-1 font-mono text-[11px] text-gray-500 dark:text-gray-400 break-all">
        {entry.command}
      </p>
    </div>
    <div class="flex items-center gap-2 shrink-0">
      <!-- Fixed width so the two buttons line up down the list rather than
           shifting with the length of each row's status word. -->
      <span class="w-32 flex justify-end">
        <StatusPill tone={status.tone} label={status.label} />
      </span>
      <button
        type="button"
        onclick={onedit}
        disabled={entry.managed}
        title={entry.managed ? m.sites_cron_managedHint() : undefined}
        class={action}
      >
        {m.sites_cron_edit()}
      </button>
      <button
        type="button"
        onclick={ondelete}
        disabled={entry.managed}
        title={entry.managed ? m.sites_cron_managedHint() : undefined}
        class="{action} hover:text-servlo-red hover:border-servlo-red/40"
      >
        {m.sites_cron_delete()}
      </button>
    </div>
  </div>

  <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-gray-500">
    <!-- Label then value, and the schedule monospace: "*/15 * * * *" in a
         proportional face collapses into something an operator cannot read
         back to check. -->
    <span title={entry.calendar}>
      {m.sites_cron_runs()} <span class="font-mono text-gray-500 dark:text-gray-400">{entry.schedule}</span>
    </span>
    {#if entry.next_run && !entry.disabled}
      <span>{m.sites_cron_nextRun()} {when(entry.next_run)}</span>
    {/if}
    {#if entry.last_run}
      <span>{m.sites_cron_ranAt()} {when(entry.last_run.at)}</span>
    {/if}
    {#if entry.last_run && !entry.last_run.ok && !entry.last_run.running}
      <span class="text-servlo-red">
        {m.sites_cron_exit({ result: entry.last_run.result ?? '', code: entry.last_run.exit_code })}
      </span>
    {/if}
    <span class="font-mono">{entry.unit}</span>
  </div>

  {#if entry.last_run}
    {#if output.length > 0}
      <button
        type="button"
        onclick={() => (showOutput = !showOutput)}
        class="mt-1.5 text-[11px] text-servlo-red hover:text-servlo-redhov transition-colors"
      >
        {showOutput ? m.sites_cron_hideOutput() : m.sites_cron_showOutput()}
      </button>
      {#if showOutput}
        <pre
          class="mt-1.5 max-h-64 overflow-y-auto rounded-md bg-gray-900 px-3 py-2 font-mono text-[11px] leading-relaxed text-gray-200 whitespace-pre-wrap">{output.join('\n')}</pre>
      {/if}
    {:else if !entry.capture_output}
      <p class="mt-1.5 text-[11px] text-gray-400 dark:text-gray-500">{m.sites_cron_noCapture()}</p>
    {/if}
  {/if}
</div>
