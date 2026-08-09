<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { applyPermissions, loadPermissionPlan, type PermissionPlan } from '$stores/files';
  import { m } from '../paraglide/messages.js';

  // The plan is shown before anything is applied, rule by rule, because "fix
  // permissions" is otherwise a button that runs an unspecified recursive chmod
  // over somebody's live site.

  const target = $derived($modal.filePermissions);

  let plan = $state<PermissionPlan | null>(null);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let applied = $state(0);

  const ruleLabels: Record<string, () => string> = {
    directories: m.files_rule_directories,
    files: m.files_rule_files,
    executables: m.files_rule_executables,
    secrets: m.files_rule_secrets
  };
  // The reason is rendered from the rule's name rather than the server's own
  // English sentence, so it is translated like every other string here.
  const ruleReasons: Record<string, () => string> = {
    directories: m.files_reason_directories,
    files: m.files_reason_files,
    executables: m.files_reason_executables,
    secrets: m.files_reason_secrets
  };

  $effect(() => {
    const domain = target?.domain;
    if (!domain) return;
    loading = true;
    loadPermissionPlan(domain)
      .then((p) => {
        plan = p;
        error = p.error ?? '';
      })
      .catch((e: unknown) => (error = e instanceof Error ? e.message : String(e)))
      .finally(() => (loading = false));
  });

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function apply() {
    if (!target) return;
    busy = true;
    error = '';
    try {
      const res = await applyPermissions(target.domain);
      if (res.error) {
        error = res.error;
        return;
      }
      plan = res;
      applied = res.applied;
      target.onApplied();
    } finally {
      busy = false;
    }
  }
</script>

<Modal open title={m.files_permissionsTitle()} onclose={safeClose} size="lg">
  <div class="px-5 py-4 space-y-3">
    <p class="text-sm text-gray-700 dark:text-gray-300">{m.files_permissionsIntro()}</p>

    {#if loading}
      <p class="text-xs text-gray-400">{m.common_loading()}</p>
    {:else if plan}
      <ul class="divide-y divide-gray-100 dark:divide-servlo-border rounded-sm border border-gray-200 dark:border-servlo-border">
        {#each plan.rules as rule (rule.name)}
          <li class="flex items-start gap-3 px-3 py-2">
            <code
              class="shrink-0 mt-0.5 text-xs font-mono px-1.5 py-0.5 rounded-sm bg-gray-100 dark:bg-white/5 text-gray-700 dark:text-gray-300"
              >{rule.octal}</code
            >
            <div class="min-w-0 flex-1">
              <p class="text-xs font-medium text-gray-900 dark:text-gray-100">
                {(ruleLabels[rule.name] ?? (() => rule.name))()}
              </p>
              <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">
                {(ruleReasons[rule.name] ?? (() => rule.reason))()}
              </p>
              {#if rule.samples && rule.samples.length > 0}
                <p class="text-[10px] font-mono text-gray-400 dark:text-gray-600 truncate">
                  {rule.samples.join(', ')}
                </p>
              {/if}
            </div>
            <span class="shrink-0 text-[11px] tabular-nums text-gray-500 dark:text-gray-400">
              {m.files_ruleCount({ count: rule.count, changes: rule.changes })}
            </span>
          </li>
        {/each}
      </ul>

      {#if applied > 0}
        <p class="text-xs text-emerald-600 dark:text-emerald-400">
          {m.files_permissionsApplied({ count: applied })}
        </p>
      {:else if plan.changes === 0}
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.files_permissionsNoChange()}</p>
      {/if}
      {#if plan.skipped > 0}
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {m.files_permissionsSkipped({ count: plan.skipped })}
        </p>
      {/if}
      {#if plan.truncated}
        <p class="text-xs text-amber-600 dark:text-amber-400">{m.files_permissionsPartial()}</p>
      {/if}
      {#if plan.failed && plan.failed.length > 0}
        <p class="text-xs text-red-500">
          {m.files_permissionsFailed({ paths: plan.failed.join(', ') })}
        </p>
      {/if}
    {/if}

    {#if error}
      <p class="text-xs text-red-500">{error}</p>
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    <DetailButton
      tone="primary"
      onclick={apply}
      loading={busy}
      disabled={busy || loading || !plan || plan.changes === 0}
    >
      {m.files_permissionsApply()}
    </DetailButton>
  {/snippet}
</Modal>
