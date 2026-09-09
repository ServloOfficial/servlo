<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import {
    loadSiteBackups,
    runBackup,
    verifyBackup,
    scheduleBackup,
    unscheduleBackup,
    type SiteBackups
  } from '$stores/backups';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let info = $state<SiteBackups | null>(null);
  let loading = $state(true);
  let busy = $state('');
  let notice = $state('');
  let error = $state('');

  // The schedule form is collapsed until asked for, so a site that is already
  // arranged shows what it does rather than a form nobody is filling in.
  let editing = $state(false);
  let when = $state('daily');
  let checkWeekly = $state(true);
  let daily = $state(7);
  let weekly = $state(4);
  let monthly = $state(3);

  async function load() {
    loading = true;
    info = await loadSiteBackups(site.domain);
    loading = false;
    if (info) {
      when = info.schedule || 'daily';
      checkWeekly = !!info.verify;
      daily = info.keep.daily;
      weekly = info.keep.weekly;
      monthly = info.keep.monthly;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function act(kind: string, run: () => Promise<{ ok: boolean; error?: string; note?: string }>) {
    busy = kind;
    notice = '';
    error = '';
    const res = await run();
    busy = '';
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    return res;
  }

  async function takeOne() {
    const res = await act('run', () => runBackup(site.domain));
    if (res) {
      const r = res as { archive?: string; files?: number; pruned?: number; note?: string };
      notice = m.backups_took({ archive: r.archive ?? '', files: r.files ?? 0 });
      if (r.pruned) notice += ' ' + m.backups_pruned({ n: r.pruned });
      if (r.note) notice += ' ' + r.note;
      await load();
    }
  }

  async function check() {
    const res = await act('verify', () => verifyBackup(site.domain));
    if (res) {
      const r = res as { tables?: number; note?: string };
      const tables = r.tables ?? 0;
      const verified = tables === 1 ? m.backups_verifiedOne({ n: tables }) : m.backups_verified({ n: tables });
      notice = r.note ? r.note : verified;
    }
  }

  async function save() {
    const res = await act('schedule', () =>
      scheduleBackup(site.domain, when, checkWeekly ? 'Sun *-*-* 04:00:00' : '', { daily, weekly, monthly })
    );
    if (res) {
      editing = false;
      await load();
    }
  }

  async function stop() {
    const res = await act('unschedule', () => unscheduleBackup(site.domain));
    if (res) await load();
  }

  function size(bytes: number): string {
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let n = bytes;
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return `${i === 0 ? n : n.toFixed(1)} ${units[i]}`;
  }

  function when_(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  const scheduled = $derived(!!info?.schedule);
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.backups_title()}</h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.backups_desc()}</p>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400 dark:text-gray-500">{m.common_loading()}</p>
  {:else if info}
    <!-- What is arranged, said in one line, because that is the question. -->
    <div class="mt-4 flex items-baseline gap-2 flex-wrap">
      <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.backups_scheduleLabel()}</span>
      {#if scheduled}
        <span class="font-mono text-xs text-gray-800 dark:text-gray-100">{info.schedule}</span>
        {#if info.disabled}
          <span class="text-[11px] px-1.5 py-0.5 rounded bg-amber-50 dark:bg-amber-500/10 text-amber-600 dark:text-amber-400"
            >{m.backups_switchedOff()}</span
          >
        {/if}
      {:else}
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.backups_notScheduled()}</span>
      {/if}
    </div>

    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {#if info.verify}
        {m.backups_checkOn({ schedule: info.verify })}
      {:else}
        {m.backups_noCheck()}
      {/if}
    </p>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500">
      {m.backups_keeping({ daily: info.keep.daily, weekly: info.keep.weekly, monthly: info.keep.monthly })}
    </p>

    <div class="mt-3 flex items-center gap-2 flex-wrap">
      <button
        type="button"
        onclick={takeOne}
        disabled={!!busy}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {busy === 'run' ? m.backups_takingOne() : m.backups_takeOne()}
      </button>
      <button
        type="button"
        onclick={check}
        disabled={!!busy || info.archives.length === 0}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
      >
        {busy === 'verify' ? m.backups_checking() : m.backups_check()}
      </button>
      <button
        type="button"
        onclick={() => (editing = !editing)}
        disabled={!!busy}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
      >
        {scheduled ? m.backups_changeSchedule() : m.backups_addSchedule()}
      </button>
      {#if scheduled}
        <button
          type="button"
          onclick={stop}
          disabled={!!busy}
          class="text-xs text-gray-500 hover:text-servlo-red disabled:opacity-50 transition-colors"
        >
          {m.backups_stop()}
        </button>
      {/if}
    </div>

    {#if editing}
      <div class="mt-4 pt-4 border-t border-gray-100 dark:border-servlo-border">
        <label class="block text-xs font-medium text-gray-700 dark:text-gray-200" for="backup-when">
          {m.backups_whenLabel()}
        </label>
        <input
          id="backup-when"
          bind:value={when}
          placeholder="daily"
          class="mt-1.5 w-full max-w-sm px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card font-mono text-xs text-gray-700 dark:text-gray-200"
        />
        <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{m.backups_whenHint()}</p>

        <!-- Labelled as a group: three bare boxes headed Daily, Weekly and
             Monthly directly under a schedule field read as three more
             schedules rather than as retention counts. -->
        <div class="mt-3 text-xs font-medium text-gray-700 dark:text-gray-200">{m.backups_keepLabel()}</div>
        <div class="mt-1.5 grid grid-cols-3 gap-3 max-w-xs">
          {#each [{ id: 'keep-daily', label: m.backups_keepDaily(), get: () => daily, set: (v: number) => (daily = v) }, { id: 'keep-weekly', label: m.backups_keepWeekly(), get: () => weekly, set: (v: number) => (weekly = v) }, { id: 'keep-monthly', label: m.backups_keepMonthly(), get: () => monthly, set: (v: number) => (monthly = v) }] as f (f.id)}
            <div>
              <label class="block text-[11px] text-gray-500 dark:text-gray-400" for={f.id}>{f.label}</label>
              <input
                id={f.id}
                type="number"
                min="0"
                value={f.get()}
                oninput={(e) => f.set(Number((e.currentTarget as HTMLInputElement).value))}
                class="mt-1.5 w-full px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200"
              />
            </div>
          {/each}
        </div>

        <label class="mt-3 flex items-start gap-2 text-xs text-gray-700 dark:text-gray-200">
          <input type="checkbox" bind:checked={checkWeekly} class="mt-0.5" />
          <span>
            {m.backups_verifyWeekly()}
            <span class="block text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
              {m.backups_verifyWeeklyHint()}
            </span>
          </span>
        </label>

        <div class="mt-3 flex items-center gap-3">
          <button
            type="button"
            onclick={save}
            disabled={!!busy}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
          >
            {m.backups_save()}
          </button>
          <button
            type="button"
            onclick={() => (editing = false)}
            class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
          >
            {m.common_cancel()}
          </button>
        </div>
      </div>
    {/if}

    <!-- What is actually on disk. A schedule says what was asked for; this says
         what a restore could use, and the two disagreeing is the thing to see. -->
    <div class="mt-5 pt-4 border-t border-gray-100 dark:border-servlo-border">
      {#if info.archives.length === 0}
        <!-- No count heading here: "0 on this server" above "None yet" says the
             same thing twice, and the sentence is the one worth reading. -->
        <p class="text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{m.backups_noneYet()}</p>
      {:else}
        <div class="text-xs font-medium text-gray-700 dark:text-gray-200">
          {info.archives.length === 1
            ? m.backups_onDiskOne()
            : m.backups_onDiskMany({ n: info.archives.length })}
        </div>
        <table class="mt-2 w-full text-xs">
          <tbody>
            {#each info.archives as a (a.name)}
              <tr class="border-t border-gray-100 dark:border-servlo-border first:border-t-0">
                <td class="w-full py-1.5 pr-3 font-mono text-gray-700 dark:text-gray-300 truncate">{a.name}</td>
                <td class="py-1.5 pr-3 text-right tabular-nums text-gray-500 dark:text-gray-400 whitespace-nowrap"
                  >{size(a.size)}</td
                >
                <td class="py-1.5 text-right tabular-nums text-gray-400 dark:text-gray-500 whitespace-nowrap"
                  >{when_(a.taken)}</td
                >
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if info.archives.length > 0}
        <!-- "These can only be opened with…" needs a these. With no archives it
             refers to nothing, and the card is quieter without it. -->
        <p class="mt-2 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
          {m.backups_keyNote({ path: info.key_path })}
        </p>
      {/if}
    </div>

    {#if notice}
      <p class="mt-3 text-xs text-green-600 dark:text-green-400 leading-relaxed">{notice}</p>
    {/if}
    {#if error}
      <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap leading-relaxed">{error}</p>
    {/if}
  {/if}
</SettingsCard>
