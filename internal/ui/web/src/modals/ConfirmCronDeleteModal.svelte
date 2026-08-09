<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { deleteSiteCron } from '$stores/cron';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.cronDelete);

  let busy = $state(false);
  let error = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target) return;
    busy = true;
    error = '';
    const res = await deleteSiteCron(target.domain, target.id);
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
  title={target ? m.sites_cron_deleteTitle({ name: target.name }) : ''}
  onclose={safeClose}
  size="sm"
>
  <div class="px-5 py-4 space-y-3">
    {#if target}
      <p class="text-sm text-gray-700 dark:text-gray-300">{m.sites_cron_deleteBody()}</p>
      {#if error}
        <p class="text-xs text-servlo-red">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="danger" onclick={confirm} loading={busy} disabled={busy}>
        {m.sites_cron_delete()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
