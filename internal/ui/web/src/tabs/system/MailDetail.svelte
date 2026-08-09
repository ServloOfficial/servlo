<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import SMTPSettingsForm from '$components/SMTPSettingsForm.svelte';
  import {
    loadPanelSMTP,
    savePanelSMTP,
    removePanelSMTP,
    testPanelSMTP,
    type SMTPStatus
  } from '$stores/smtp';
  import { onMount } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  // The panel's own account, separate from any site's. Alerts about
  // certificates, backups and deploys go out through this one, addressed to its
  // own sender address, so an operator who wants them elsewhere points that
  // address at the inbox they read.

  let status = $state<SMTPStatus>({ configured: false, settings: {} });
  let loading = $state(true);

  async function load() {
    loading = true;
    status = await loadPanelSMTP();
    loading = false;
  }

  onMount(() => {
    void load();
  });
</script>

{#snippet state()}
  {#if loading}
    <span class="text-xs text-gray-400">{m.common_loading()}</span>
  {:else if status.configured}
    <StatusPill tone="ok" label={m.smtp_panel_on()} />
  {:else}
    <StatusPill tone="warn" label={m.smtp_panel_off()} />
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.smtp_panel_title()} trailing={state} />

  <div class="flex-1 min-h-0 overflow-y-auto">
    <div class="max-w-3xl">
      <p class="px-3 sm:px-5 pt-3 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
        {m.smtp_panel_intro()}
      </p>
      <p class="px-3 sm:px-5 pt-2 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
        {m.smtp_panel_alertsTo()}
      </p>
    </div>

    <section class="px-3 sm:px-5 py-4 max-w-3xl">
      <SMTPSettingsForm
        {status}
        {loading}
        defaultRecipient={status.settings.from_address ?? ''}
        onsave={savePanelSMTP}
        ontest={testPanelSMTP}
        onremove={removePanelSMTP}
        onchanged={load}
      />
    </section>
  </div>
</DetailPanel>
