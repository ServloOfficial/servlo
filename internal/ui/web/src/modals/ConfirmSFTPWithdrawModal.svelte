<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { withdrawSFTPKey } from '$stores/sftp';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.sftpWithdraw);

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
    const res = await withdrawSFTPKey(target.fingerprint);
    busy = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    target.onWithdrawn();
    closeModal();
  }
</script>

<Modal
  open
  title={target ? m.sftp_withdrawTitle({ label: target.label }) : ''}
  onclose={safeClose}
  size="sm"
>
  <div class="px-5 py-4 space-y-3">
    {#if target}
      <p class="text-sm text-gray-700 dark:text-gray-300">{m.sftp_withdrawBody()}</p>
      <p class="text-[11px] font-mono text-gray-400 dark:text-gray-600 break-all">
        {target.fingerprint}
      </p>
      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    <DetailButton tone="danger" onclick={confirm} loading={busy} disabled={busy}>
      {m.sftp_withdraw()}
    </DetailButton>
  {/snippet}
</Modal>
