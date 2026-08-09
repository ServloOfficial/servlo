<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { deleteFileEntry } from '$stores/files';
  import { m } from '../paraglide/messages.js';

  // Typed confirmation, not a second button. This deletes from a live site with
  // no undo and no backup, and a modal you can dismiss with the same reflex that
  // opened it is not friction.

  const target = $derived($modal.fileDelete);

  let typed = $state('');
  let busy = $state(false);
  let error = $state('');

  const matches = $derived(!!target && typed.trim() === target.name);

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target || !matches) return;
    busy = true;
    error = '';
    const res = await deleteFileEntry(target.domain, target.path);
    busy = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    target.onDeleted();
    closeModal();
  }
</script>

<Modal
  open
  title={target ? m.files_deleteTitle({ name: target.name }) : ''}
  onclose={safeClose}
  size="sm"
>
  <div class="px-5 py-4 space-y-3">
    {#if target}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {target.dir
          ? m.files_deleteFolderBody({ name: target.name })
          : m.files_deleteBody({ name: target.name })}
      </p>
      <label class="block space-y-1">
        <span class="text-xs text-gray-500 dark:text-gray-400"
          >{m.files_deleteConfirm({ name: target.name })}</span
        >
        <input
          type="text"
          bind:value={typed}
          disabled={busy}
          autocomplete="off"
          spellcheck="false"
          class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-sm font-mono text-gray-900 dark:text-gray-100 disabled:opacity-50"
        />
      </label>
      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    <DetailButton
      tone="danger"
      onclick={confirm}
      loading={busy}
      disabled={busy || !matches}
    >
      {m.files_delete()}
    </DetailButton>
  {/snippet}
</Modal>
