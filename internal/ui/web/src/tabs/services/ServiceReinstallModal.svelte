<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import type { Service } from '$stores/services';

  interface Props {
    open: boolean;
    svc: Service;
    onclose: () => void;
    onconfirm: (opts: { resetData: boolean }) => void | Promise<void>;
  }
  let { open, svc, onclose, onconfirm }: Props = $props();

  let resetData = $state(false);
  let submitting = $state(false);

  const dependents = $derived(svc.site_count || 0);

  $effect(() => {
    if (open) {
      resetData = false;
      submitting = false;
    }
  });

  async function confirm() {
    submitting = true;
    try {
      await onconfirm({ resetData });
    } finally {
      submitting = false;
      onclose();
    }
  }
</script>

<Modal {open} {onclose} title={`Reinstall ${svc.name}`} size="sm">
  <div class="px-5 py-4 space-y-3">
    <p class="text-sm text-gray-600 dark:text-gray-400">
      Stops, removes, and reinstalls {svc.name} at the current version. Use this when a service update produced data incompatible with the new image.
    </p>

    <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
      <input
        type="checkbox"
        bind:checked={resetData}
        class="mt-0.5 w-4 h-4 rounded border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-bg text-lerd-red focus:ring-lerd-red/40 cursor-pointer"
      />
      <span>
        Reset to clean state (remove data)
        <span class="block text-[11px] text-gray-500 dark:text-gray-400">
          Renames the data dir aside (recoverable as <span class="font-mono">{svc.name}.pre-remove-&lt;ts&gt;</span>) and recreates databases or buckets for {dependents} linked site{dependents === 1 ? '' : 's'} on the fresh service.
        </span>
      </span>
    </label>

    {#if resetData && dependents > 0}
      <div class="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-900/40 rounded px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
        Data will be wiped. The reinstall will recreate empty databases or buckets for the linked site{dependents === 1 ? '' : 's'}, but their previous content is gone (the rename-aside copy can be restored manually).
      </div>
    {/if}
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      class="text-xs px-3 py-1.5 rounded border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
    >Cancel</button>
    <button
      type="button"
      onclick={confirm}
      disabled={submitting}
      class="text-xs px-3 py-1.5 rounded {resetData ? 'bg-lerd-red hover:bg-lerd-redhov' : 'bg-lerd-red/80 hover:bg-lerd-red'} text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
    >{submitting ? 'Reinstalling…' : resetData ? 'Reinstall + reset data' : 'Reinstall'}</button>
  {/snippet}
</Modal>
