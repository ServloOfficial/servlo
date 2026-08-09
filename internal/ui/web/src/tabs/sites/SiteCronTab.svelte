<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import SiteCronForm from './SiteCronForm.svelte';
  import SiteCronRow from './SiteCronRow.svelte';
  import { openCronDeleteModal } from '$stores/modals';
  import { loadSiteCron, saveSiteCron, setPseudoCron, type CronDraft, type SiteCron } from '$stores/cron';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let data = $state<SiteCron | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  // The id being edited, '' for the new-entry form, null for neither. Three
  // states rather than two booleans, because opening one has to close the other.
  let editing = $state<string | null>(null);
  let saving = $state(false);
  let formError = $state('');
  let switching = $state(false);
  let switchError = $state('');

  async function load() {
    loading = true;
    loadError = '';
    try {
      data = await loadSiteCron(site.domain);
    } catch {
      loadError = m.sites_cron_loadFailed();
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function save(draft: CronDraft) {
    saving = true;
    formError = '';
    const res = await saveSiteCron(site.domain, draft);
    saving = false;
    if (!res.ok) {
      formError = res.error ?? m.common_failed();
      return;
    }
    editing = null;
    await load();
  }

  async function togglePseudo(replace: boolean) {
    switching = true;
    switchError = '';
    const res = await setPseudoCron(site.domain, replace);
    switching = false;
    if (!res.ok) {
      switchError = res.error ?? m.common_failed();
      return;
    }
    await load();
  }

  function confirmDelete(id: string, name: string) {
    openCronDeleteModal({ domain: site.domain, id, name, onDeleted: () => void load() });
  }

  const pseudo = $derived(data?.pseudo_cron);
  const entries = $derived(data?.entries ?? []);
</script>

<div class="flex-1 overflow-y-auto px-6 py-4 space-y-4">
  {#if pseudo?.available}
    <SettingsCard>
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
            {m.sites_cron_pseudoTitle()}
          </h2>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
            {pseudo.description}
          </p>
        </div>
        <StatusPill
          tone={pseudo.replaced ? 'ok' : 'warn'}
          label={pseudo.replaced ? m.sites_cron_pseudoOn() : m.sites_cron_pseudoOff()}
        />
      </div>

      <p class="mt-3 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
        {m.sites_cron_pseudoDetail({
          label: pseudo.label ?? '',
          constant: pseudo.constant ?? '',
          file: pseudo.file ?? ''
        })}
      </p>
      <!-- The command and its schedule are code: a cron line in a proportional
           face is a row of asterisks, and this is the part an operator checks. -->
      <p class="mt-1 font-mono text-[11px] text-gray-500 dark:text-gray-400">
        {pseudo.command}
      </p>
      <p class="text-[11px] text-gray-400 dark:text-gray-500">
        {m.sites_cron_runs()} <span class="font-mono">{pseudo.schedule}</span>
      </p>

      <div class="mt-4 flex items-center gap-3">
        {#if pseudo.replaced}
          <button
            type="button"
            onclick={() => togglePseudo(false)}
            disabled={switching}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-200 dark:border-servlo-border text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
          >
            {switching ? m.sites_cron_pseudoBusy() : m.sites_cron_pseudoTurnOff()}
          </button>
        {:else}
          <button
            type="button"
            onclick={() => togglePseudo(true)}
            disabled={switching || !data?.supported}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
          >
            {switching ? m.sites_cron_pseudoBusy() : m.sites_cron_pseudoTurnOn()}
          </button>
        {/if}
      </div>

      {#if switchError}
        <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{switchError}</p>
      {/if}
    </SettingsCard>
  {/if}

  <SettingsCard>
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
      {m.sites_cron_title()}
    </h2>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
      {m.sites_cron_desc()}
    </p>

    {#if loading}
      <p class="mt-4 text-xs text-gray-400">{m.common_loading()}</p>
    {:else if loadError}
      <p class="mt-4 text-xs text-servlo-red">{loadError}</p>
    {:else if !data?.supported}
      <p class="mt-4 text-xs text-amber-600 dark:text-amber-400">{data?.unsupported}</p>
    {:else}
      {#if entries.length === 0}
        <p class="mt-4 text-xs text-gray-400 dark:text-gray-500">{m.sites_cron_empty()}</p>
      {:else}
        <div class="mt-4">
          {#each entries as entry (entry.id)}
            {#if editing === entry.id}
              <SiteCronForm
                {entry}
                {saving}
                error={formError}
                onsave={save}
                oncancel={() => (editing = null)}
              />
            {:else}
              <SiteCronRow
                {entry}
                onedit={() => {
                  formError = '';
                  editing = entry.id;
                }}
                ondelete={() => confirmDelete(entry.id, entry.name)}
              />
            {/if}
          {/each}
        </div>
      {/if}

      {#if editing === ''}
        <SiteCronForm
          {saving}
          error={formError}
          onsave={save}
          oncancel={() => (editing = null)}
        />
      {:else}
        <button
          type="button"
          onclick={() => {
            formError = '';
            editing = '';
          }}
          class="mt-4 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white transition-colors"
        >
          {m.sites_cron_add()}
        </button>
      {/if}
    {/if}
  </SettingsCard>
</div>
