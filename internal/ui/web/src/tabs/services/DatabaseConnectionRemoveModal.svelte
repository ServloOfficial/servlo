<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import type { DBConnection } from '$stores/dbConnections';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    connection: DBConnection;
    onclose: () => void;
    onconfirm: () => void | Promise<void>;
  }
  let { open, connection, onclose, onconfirm }: Props = $props();

  let typedName = $state('');
  let submitting = $state(false);

  const canConfirm = $derived(!submitting && typedName.trim() === connection.name);

  $effect(() => {
    if (open) {
      typedName = '';
      submitting = false;
    }
  });

  async function confirm() {
    if (!canConfirm) return;
    submitting = true;
    try {
      await onconfirm();
    } finally {
      submitting = false;
      onclose();
    }
  }
</script>

<Modal {open} {onclose} title={m.dbconn_removeTitle({ name: connection.name })} size="sm">
  <div class="px-5 py-4 space-y-3">
    {#if connection.sites.length > 0}
      <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-900/40 rounded-sm px-3 py-2 text-xs text-red-700 dark:text-red-300">
        <p class="font-medium mb-1">{m.dbconn_removeInUse({ count: connection.sites.length })}</p>
        <ul class="list-disc list-inside space-y-0.5">
          {#each connection.sites as domain (domain)}
            <li class="font-mono">{domain}</li>
          {/each}
        </ul>
      </div>
    {/if}

    <p class="text-sm text-gray-600 dark:text-gray-400">{m.dbconn_removeBody()}</p>

    <div class="space-y-1">
      <label for="dbconn-remove-confirm" class="text-xs text-gray-600 dark:text-gray-400">
        {m.services_confirm_typeBefore()}
        <span class="font-mono font-medium text-gray-800 dark:text-gray-200">{connection.name}</span>
        {m.services_confirm_typeAfter()}
      </label>
      <input
        id="dbconn-remove-confirm"
        type="text"
        bind:value={typedName}
        class="w-full text-sm bg-white dark:bg-servlo-bg border border-gray-200 dark:border-servlo-border rounded-sm px-2.5 py-1.5 text-gray-700 dark:text-gray-300 focus:outline-hidden focus:border-servlo-red/50"
        autocomplete="off"
      />
    </div>
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      class="text-xs px-3 py-1.5 rounded-sm border border-gray-200 dark:border-servlo-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={confirm}
      disabled={!canConfirm}
      class="text-xs px-3 py-1.5 rounded-sm bg-servlo-red hover:bg-servlo-redhov text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
    >{submitting ? m.dbconn_removing() : m.common_remove()}</button>
  {/snippet}
</Modal>
