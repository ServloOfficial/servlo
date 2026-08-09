<script lang="ts">
  import type { CronDraft, CronEntry } from '$stores/cron';
  import { m } from '../../paraglide/messages.js';

  // One form for both adding and editing, because they are the same four
  // fields and a second copy of them is a second place for the schedule hint
  // to go out of date.
  interface Props {
    /** The entry being edited, or undefined when this is a new one. */
    entry?: CronEntry;
    saving: boolean;
    error: string;
    onsave: (draft: CronDraft) => void;
    oncancel: () => void;
  }
  let { entry, saving, error, onsave, oncancel }: Props = $props();

  let name = $state(entry?.name ?? '');
  let command = $state(entry?.command ?? '');
  let schedule = $state(entry?.schedule ?? '');
  let capture = $state(entry ? entry.capture_output : true);
  let disabled = $state(entry?.disabled ?? false);

  // The forms the server accepts, one of each kind, so the operator can copy
  // the shape rather than discover it from a refusal.
  const SCHEDULE_EXAMPLES = ['*/5 * * * *', '0 3 * * 1-5', 'daily', 'Mon *-*-* 02:00:00'];

  const field =
    'mt-1 w-full px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200 focus:outline-none focus:ring-1 focus:ring-servlo-red';
  const label = 'text-xs font-medium text-gray-700 dark:text-gray-200';
  const hint = 'mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed';

  const ready = $derived(name.trim() !== '' && command.trim() !== '' && schedule.trim() !== '');

  function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!ready || saving) return;
    onsave({
      id: entry?.id,
      name: name.trim(),
      command: command.trim(),
      schedule: schedule.trim(),
      capture_output: capture,
      disabled
    });
  }
</script>

<form
  onsubmit={submit}
  class="mt-3 rounded-lg border border-gray-200 dark:border-servlo-border p-3 space-y-3"
>
  <div>
    <label class={label} for="cron-name">{m.sites_cron_name()}</label>
    <input
      id="cron-name"
      type="text"
      bind:value={name}
      placeholder={m.sites_cron_namePlaceholder()}
      class={field}
    />
  </div>

  <div>
    <label class={label} for="cron-command">{m.sites_cron_command()}</label>
    <input
      id="cron-command"
      type="text"
      spellcheck="false"
      bind:value={command}
      placeholder={m.sites_cron_commandPlaceholder()}
      class="{field} font-mono"
    />
    <p class={hint}>{m.sites_cron_commandHint()}</p>
  </div>

  <div>
    <label class={label} for="cron-schedule">{m.sites_cron_schedule()}</label>
    <input
      id="cron-schedule"
      type="text"
      spellcheck="false"
      bind:value={schedule}
      placeholder={m.sites_cron_schedulePlaceholder()}
      class="{field} font-mono"
    />
    <p class={hint}>{m.sites_cron_scheduleHint()}</p>
    <!-- The examples are code, not prose, so they are monospace and the same
         in every language: "*/5 * * * *" set in a proportional face is a row
         of asterisks nobody can count. -->
    <ul class="mt-1 space-y-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400">
      {#each SCHEDULE_EXAMPLES as example (example)}
        <li>{example}</li>
      {/each}
    </ul>
  </div>

  <div class="space-y-1.5">
    <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-gray-400">
      <input type="checkbox" bind:checked={capture} />
      {m.sites_cron_capture()}
    </label>
    <p class="{hint} ml-5">{m.sites_cron_captureHint()}</p>
    <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-gray-400">
      <input type="checkbox" bind:checked={disabled} />
      {m.sites_cron_disabled()}
    </label>
  </div>

  {#if error}
    <p class="text-xs text-servlo-red whitespace-pre-wrap">{error}</p>
  {/if}

  <div class="flex items-center gap-3">
    <button
      type="submit"
      disabled={saving || !ready}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
    >
      {saving ? m.sites_cron_saving() : m.sites_cron_save()}
    </button>
    <button
      type="button"
      onclick={oncancel}
      disabled={saving}
      class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 disabled:opacity-50 transition-colors"
    >
      {m.common_cancel()}
    </button>
  </div>
</form>
