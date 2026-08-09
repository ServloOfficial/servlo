<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import SettingsField from '$components/SettingsField.svelte';
  import SiteDatabaseUserCard from './SiteDatabaseUserCard.svelte';
  import {
    dbConnections,
    loadDBConnections,
    assignConnection,
    connectionLocation,
    connectionForSite
  } from '$stores/dbConnections';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  $effect(() => {
    // Reload per site so the site lists each connection carries are the ones
    // this row is reading itself out of.
    void site.domain;
    void loadDBConnections();
  });

  // Read back from the connections rather than kept alongside them, so the row
  // shows what the server says a moment after an assignment lands.
  const current = $derived(connectionForSite($dbConnections.connections, site.domain));

  let choice = $state('');
  $effect(() => {
    choice = current;
  });

  async function choose(name: string) {
    if (name === current) return;
    saving = true;
    saved = false;
    error = '';
    const res = await assignConnection(site.domain, name);
    saving = false;
    if (res.ok) {
      saved = true;
      setTimeout(() => (saved = false), 2000);
    } else {
      // The server kept the site where it was, so the box goes back too rather
      // than showing a connection the site is not on.
      error = res.error || m.common_failed();
      choice = current;
    }
  }
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.dbconn_site_title()}</h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.dbconn_site_desc()}
  </p>

  <div class="mt-4">
    <SettingsField
      label={m.dbconn_site_label()}
      hint={m.dbconn_site_hint()}
      forId="site-db-connection"
    >
      <div class="flex items-center gap-3">
        <select
          id="site-db-connection"
          disabled={saving}
          bind:value={choice}
          onchange={() => choose(choice)}
          class="px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200 disabled:opacity-50"
        >
          <option value="">{m.dbconn_site_useDefault()}</option>
          {#each $dbConnections.connections as conn (conn.name)}
            <option value={conn.name}>{conn.name} ({connectionLocation(conn)})</option>
          {/each}
        </select>
        {#if saved}
          <span class="text-xs text-green-600 dark:text-green-400">{m.sites_phpSettings_saved()}</span>
        {/if}
      </div>
    </SettingsField>
  </div>

  {#if error}
    <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{error}</p>
  {/if}

  <SiteDatabaseUserCard {site} />
</SettingsCard>
