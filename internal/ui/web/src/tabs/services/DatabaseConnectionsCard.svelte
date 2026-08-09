<script lang="ts">
  import { onMount } from 'svelte';
  import Badge from '$components/Badge.svelte';
  import Icon from '$components/Icon.svelte';
  import SettingsCard from '$components/SettingsCard.svelte';
  import DatabaseConnectionForm from './DatabaseConnectionForm.svelte';
  import DatabaseConnectionRemoveModal from './DatabaseConnectionRemoveModal.svelte';
  import {
    dbConnections,
    loadDBConnections,
    removeConnection,
    setDefaultConnection,
    connectionLocation,
    isLocalConnection,
    type DBConnection
  } from '$stores/dbConnections';
  import { m } from '../../paraglide/messages.js';

  let adding = $state(false);
  let removing = $state<DBConnection | null>(null);
  // The list refreshes itself after every action, so a failure has one place to
  // land rather than one per row.
  let actionError = $state('');

  onMount(() => {
    void loadDBConnections();
  });

  async function makeDefault(name: string) {
    actionError = '';
    const res = await setDefaultConnection(name);
    if (!res.ok) actionError = res.error || m.common_failed();
  }

  async function remove(name: string) {
    actionError = '';
    const res = await removeConnection(name);
    if (!res.ok) actionError = res.error || m.common_failed();
  }
</script>

<SettingsCard>
  <div class="flex items-baseline justify-between gap-3">
    <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">{m.dbconn_title()}</h2>
    {#if !adding}
      <button
        type="button"
        onclick={() => (adding = true)}
        class="inline-flex items-center gap-1 text-xs font-medium text-servlo-red hover:text-servlo-redhov transition-colors"
      >
        <Icon name="plus" class="w-3.5 h-3.5" />
        {m.dbconn_add()}
      </button>
    {/if}
  </div>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.dbconn_desc()}</p>

  {#if $dbConnections.loading && $dbConnections.connections.length === 0}
    <p class="mt-4 text-xs text-gray-400">{m.common_loading()}</p>
  {:else if $dbConnections.connections.length === 0}
    <p class="mt-4 text-xs text-gray-500 dark:text-gray-400">{m.dbconn_empty()}</p>
  {:else}
    <ul class="mt-4 divide-y divide-gray-100 dark:divide-servlo-border">
      {#each $dbConnections.connections as conn (conn.name)}
        <li class="flex items-center justify-between gap-3 py-2">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{conn.name}</span>
              {#if conn.default}
                <Badge tone="framework">{m.dbconn_defaultBadge()}</Badge>
              {/if}
            </div>
            <p class="mt-0.5 text-[11px] text-gray-400 dark:text-gray-500">
              {conn.family}
              <span class="mx-1">·</span>
              <span class="font-mono">{connectionLocation(conn)}</span>
              <span class="mx-1">·</span>
              {isLocalConnection(conn) ? m.dbconn_kindLocal() : m.dbconn_kindManaged()}
              <span class="mx-1">·</span>
              {m.dbconn_sitesOn({ count: conn.sites.length })}
            </p>
          </div>
          <div class="flex shrink-0 items-center gap-3">
            {#if !conn.default}
              <button
                type="button"
                onclick={() => makeDefault(conn.name)}
                class="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
              >
                {m.dbconn_makeDefault()}
              </button>
            {/if}
            <button
              type="button"
              onclick={() => (removing = conn)}
              class="text-xs text-gray-400 hover:text-servlo-red transition-colors"
            >
              {m.common_remove()}
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if $dbConnections.error}
    <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{$dbConnections.error}</p>
  {/if}
  {#if actionError}
    <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap">{actionError}</p>
  {/if}

  {#if adding}
    <DatabaseConnectionForm
      services={$dbConnections.services}
      onadded={() => (adding = false)}
      oncancel={() => (adding = false)}
    />
  {/if}
</SettingsCard>

{#if removing}
  <DatabaseConnectionRemoveModal
    open={true}
    connection={removing}
    onclose={() => (removing = null)}
    onconfirm={() => remove(removing?.name ?? '')}
  />
{/if}
