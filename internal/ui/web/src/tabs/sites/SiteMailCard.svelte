<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SMTPSettingsForm from '$components/SMTPSettingsForm.svelte';
  import {
    loadSiteSMTP,
    saveSiteSMTP,
    removeSiteSMTP,
    testSiteSMTP,
    type SMTPStatus
  } from '$stores/smtp';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let status = $state<SMTPStatus>({ configured: false, settings: {} });
  let loading = $state(true);

  async function load() {
    loading = true;
    status = await loadSiteSMTP(site.domain);
    loading = false;
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  const keys = $derived((status.env_keys ?? []).join(', '));
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.smtp_site_title()}</h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.smtp_site_desc()}</p>

  {#if status.note}
    <p class="mt-3 text-[11px] text-amber-600 dark:text-amber-400 leading-relaxed">{status.note}</p>
  {:else if keys}
    <p class="mt-3 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {m.smtp_site_writesKeys({ file: status.env_file || '.env', keys })}
    </p>
  {/if}

  <div class="mt-4">
    <SMTPSettingsForm
      {status}
      {loading}
      defaultRecipient={status.settings.from_address ?? ''}
      onsave={(form) => saveSiteSMTP(site.domain, form)}
      ontest={(to) => testSiteSMTP(site.domain, to)}
      onremove={() => removeSiteSMTP(site.domain)}
      onchanged={load}
    />
  </div>
</SettingsCard>
