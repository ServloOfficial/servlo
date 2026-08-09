<script lang="ts">
  import { onMount } from 'svelte';
  import Badge from '$components/Badge.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import Icon from '$components/Icon.svelte';
  import SettingsCard from '$components/SettingsCard.svelte';
  import DatabaseConnectionForm from './DatabaseConnectionForm.svelte';
  import DatabaseConnectionRemoveModal from './DatabaseConnectionRemoveModal.svelte';
  import DatabaseTrustedSources from './DatabaseTrustedSources.svelte';
  import {
    dbConnections,
    loadDBConnections,
    removeConnection,
    setDefaultConnection,
    testConnection,
    connectionMeta,
    isManagedConnection,
    type DBConnection
  } from '$stores/dbConnections';
  import { m } from '../../paraglide/messages.js';

  let adding = $state(false);
  let removing = $state<DBConnection | null>(null);
  // One row at a time: the result belongs to the connection it was asked about,
  // and a shared line would show the last answer beside every row.
  let testingName = $state('');
  let testResults = $state<Record<string, { ok: boolean; error: string }>>({});
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

  async function test(name: string) {
    testingName = name;
    const res = await testConnection(name);
    testingName = '';
    testResults = {
      ...testResults,
      [name]: { ok: res.ok, error: res.ok ? '' : res.error || m.common_failed() }
    };
  }
</script>

<section class="space-y-2.5">
  <div class="flex items-center justify-between gap-3">
    <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{m.dbconn_title()}</h2>
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

  <SettingsCard>
    <p class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{m.dbconn_desc()}</p>

    {#if $dbConnections.loading && $dbConnections.connections.length === 0}
      <p class="mt-4 text-xs text-gray-400">{m.common_loading()}</p>
    {:else if $dbConnections.connections.length === 0}
      <p class="mt-4 text-xs text-gray-500 dark:text-gray-400">{m.dbconn_empty()}</p>
    {:else}
      <ul class="mt-3 divide-y divide-gray-200 dark:divide-servlo-border">
        {#each $dbConnections.connections as conn (conn.name)}
          {@const meta = connectionMeta(conn)}
          <li class="py-2.5">
            <div class="flex items-center justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{conn.name}</span>
                  {#if conn.default}
                    <Badge tone="framework">{m.dbconn_defaultBadge()}</Badge>
                  {/if}
                </div>
                <p class="mt-0.5 text-[11px] text-gray-400 dark:text-gray-500">
                  {meta.engine}
                  <span class="mx-1">·</span>
                  {#if meta.location}
                    <span class="font-mono">{meta.location}</span>
                    <span class="mx-1">·</span>
                  {/if}
                  {meta.kind}
                  <span class="mx-1">·</span>
                  {meta.sites}
                </p>
              </div>
              <div class="flex shrink-0 items-center justify-end gap-2">
                <!-- A local service answers on a network the panel is not on, so
                     the row keeps the space rather than offering a test that
                     could only ever fail. -->
                <span class:invisible={!isManagedConnection(conn)} aria-hidden={!isManagedConnection(conn)}>
                  <DetailButton
                    tone="secondary"
                    onclick={() => test(conn.name)}
                    disabled={testingName === conn.name}
                  >
                    {testingName === conn.name ? m.dbconn_testing() : m.dbconn_test()}
                  </DetailButton>
                </span>
                <!-- The default row keeps the space its button would take, so
                     Remove stays in the same column down the whole list. -->
                <span class:invisible={conn.default} aria-hidden={conn.default}>
                  <DetailButton tone="secondary" onclick={() => makeDefault(conn.name)}>
                    {m.dbconn_makeDefault()}
                  </DetailButton>
                </span>
                <DetailButton tone="danger" onclick={() => (removing = conn)}>
                  {m.common_remove()}
                </DetailButton>
              </div>
            </div>

            {#if testResults[conn.name]}
              {#if testResults[conn.name].ok}
                <p class="mt-2 text-[11px] text-emerald-600 dark:text-emerald-500">{m.dbconn_testOk()}</p>
              {:else}
                <div class="mt-2 space-y-2">
                  <p class="text-[11px] text-servlo-red whitespace-pre-wrap">{testResults[conn.name].error}</p>
                  <!-- The address goes beside the failure rather than in the
                       message: a provider drops the packet from an address its
                       trusted sources do not hold, and this is that row. -->
                  <DatabaseTrustedSources
                    ips={$dbConnections.serverIPs}
                    error={$dbConnections.serverIPsError}
                  />
                </div>
              {/if}
            {/if}
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
        serverIPs={$dbConnections.serverIPs}
        serverIPsError={$dbConnections.serverIPsError}
        onadded={() => (adding = false)}
        oncancel={() => (adding = false)}
      />
    {/if}
  </SettingsCard>
</section>

{#if removing}
  <DatabaseConnectionRemoveModal
    open={true}
    connection={removing}
    onclose={() => (removing = null)}
    onconfirm={() => remove(removing?.name ?? '')}
  />
{/if}
