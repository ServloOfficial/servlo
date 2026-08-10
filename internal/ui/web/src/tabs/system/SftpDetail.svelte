<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Icon from '$components/Icon.svelte';
  import CommandBlock from '$components/CommandBlock.svelte';
  import { sites } from '$stores/sites';
  import { authoriseSFTPKey, loadSFTP, sftpLoaded, sftpStatus } from '$stores/sftp';
  import { openSFTPWithdrawModal } from '$stores/modals';
  import { onMount } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  // Two things on one page, and the order matters. The keys are what servlo
  // manages; the block underneath is what servlo will not run. Putting the
  // block last would let somebody authorise a key and never scroll to the part
  // that says it is not confined yet.

  let domain = $state('');
  let label = $state('');
  let key = $state('');
  let busy = $state(false);
  let error = $state('');

  const status = $derived($sftpStatus);

  onMount(() => {
    void loadSFTP();
  });

  // Default the picker to the first site once the list arrives, so the form is
  // usable without a click that only re-selects what was already showing.
  $effect(() => {
    if (!domain && $sites.length > 0) domain = $sites[0].domain;
  });

  async function authorise() {
    busy = true;
    error = '';
    const res = await authoriseSFTPKey(domain, label.trim(), key.trim());
    busy = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    label = '';
    key = '';
    await loadSFTP();
  }

  function withdraw(fingerprint: string, keyLabel: string) {
    openSFTPWithdrawModal({
      fingerprint,
      label: keyLabel,
      onWithdrawn: () => void loadSFTP()
    });
  }
</script>

{#snippet confinement()}
  {#if !$sftpLoaded}
    <span class="text-xs text-gray-400">{m.common_loading()}</span>
  {:else if status.sites.length === 0}
    <span></span>
  {:else if status.installed && !status.drifted}
    <StatusPill tone="ok" label={m.sftp_pillConfined()} title={m.sftp_confined()} />
  {:else}
    <StatusPill
      tone="warn"
      label={status.drifted ? m.sftp_pillDrifted() : m.sftp_pillNotConfined()}
      title={status.drifted ? m.sftp_drifted() : m.sftp_notConfined()}
    />
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.sftp_title()} trailing={confinement} />

  <div class="flex-1 min-h-0 overflow-y-auto">
    <p class="px-3 sm:px-5 pt-3 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
      {m.sftp_intro()}
    </p>

    {#if $sftpLoaded && status.sites.length > 0 && (!status.installed || status.drifted)}
      <div
        class="mx-3 sm:mx-5 mt-3 flex items-start gap-2 px-3 py-2 rounded-sm bg-amber-50 dark:bg-amber-900/15 border border-amber-200 dark:border-amber-900/40"
      >
        <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
        <p class="text-xs text-amber-700 dark:text-amber-300 leading-relaxed">
          {status.drifted ? m.sftp_drifted() : m.sftp_notConfined()}
        </p>
      </div>
    {/if}

    <!-- The same-account caveat is stated on the page, not only in the docs.
         "Locked to that site's directory" reads as isolation, and here it is
         not: it is a directory lock over one shared Linux account. -->
    <div
      class="mx-3 sm:mx-5 mt-3 flex items-start gap-2 px-3 py-2 rounded-sm bg-gray-50 dark:bg-white/3 border border-gray-200 dark:border-servlo-border"
    >
      <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-gray-400 dark:text-gray-500" />
      <p class="text-xs text-gray-600 dark:text-gray-300 leading-relaxed">
        {m.sftp_sameUser({ user: status.user || 'servlo' })}
      </p>
    </div>

    <section class="px-3 sm:px-5 py-4 space-y-3">
      <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{m.sftp_addKey()}</h3>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="space-y-1">
          <span class="block text-[11px] text-gray-500 dark:text-gray-400">{m.sftp_siteLabel()}</span>
          <select
            bind:value={domain}
            disabled={busy}
            class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-xs text-gray-900 dark:text-gray-100 disabled:opacity-50"
          >
            {#each $sites as s (s.domain)}
              <option value={s.domain}>{s.domain}</option>
            {/each}
          </select>
        </label>
        <label class="space-y-1">
          <span class="block text-[11px] text-gray-500 dark:text-gray-400">{m.sftp_label()}</span>
          <input
            type="text"
            bind:value={label}
            disabled={busy}
            autocomplete="off"
            spellcheck="false"
            class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-xs font-mono text-gray-900 dark:text-gray-100 disabled:opacity-50"
          />
          <span class="block text-[10px] text-gray-400 dark:text-gray-600">{m.sftp_labelHint()}</span>
        </label>
      </div>
      <label class="block space-y-1">
        <span class="block text-[11px] text-gray-500 dark:text-gray-400">{m.sftp_publicKey()}</span>
        <textarea
          bind:value={key}
          disabled={busy}
          rows="3"
          spellcheck="false"
          class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-[11px] font-mono text-gray-900 dark:text-gray-100 disabled:opacity-50"
        ></textarea>
        <span class="block text-[10px] text-gray-400 dark:text-gray-600">{m.sftp_publicKeyHint()}</span>
      </label>
      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
      <div class="flex justify-end">
        <DetailButton
          tone="primary"
          onclick={authorise}
          loading={busy}
          disabled={busy || !domain || !label.trim() || !key.trim()}
        >
          {busy ? m.sftp_authorising() : m.sftp_authorise()}
        </DetailButton>
      </div>
    </section>

    <section class="px-3 sm:px-5 pb-4 space-y-3">
      {#if !$sftpLoaded}
        <p class="text-xs text-gray-400">{m.common_loading()}</p>
      {:else if status.sites.length === 0}
        <EmptyState title={m.sftp_noKeys()} />
      {:else}
        {#each status.sites as site (site.domain)}
          <div class="rounded-sm border border-gray-200 dark:border-servlo-border">
            <div
              class="flex flex-wrap items-center justify-between gap-2 px-3 py-2 border-b border-gray-100 dark:border-servlo-border"
            >
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-900 dark:text-gray-100 truncate">
                  {site.domain}
                </p>
                <p class="text-[10px] text-gray-400 dark:text-gray-600">
                  {m.sftp_port({ port: site.port })} · {m.sftp_keyCount({ count: site.keys.length })}
                </p>
              </div>
              {#if site.path}
                <div class="flex items-center gap-1.5 min-w-0">
                  <span class="text-[10px] text-gray-400 dark:text-gray-600 shrink-0"
                    >{m.sftp_connectWith()}</span
                  >
                  <code
                    class="text-[10px] font-mono px-1.5 py-0.5 rounded-sm bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-300 truncate"
                    >sftp -P {site.port} {status.user}@{site.domain}</code
                  >
                </div>
              {:else}
                <span class="text-[10px] text-amber-600 dark:text-amber-400">{m.sftp_siteMissing()}</span>
              {/if}
            </div>
            <ul class="divide-y divide-gray-100 dark:divide-servlo-border">
              {#each site.keys as k (k.fingerprint)}
                <li class="flex items-center justify-between gap-3 px-3 py-2">
                  <div class="min-w-0">
                    <p class="text-xs text-gray-800 dark:text-gray-100 truncate">{k.label}</p>
                    <p class="text-[10px] font-mono text-gray-400 dark:text-gray-600 truncate">
                      {k.type} {k.fingerprint}
                    </p>
                  </div>
                  <DetailButton tone="danger" onclick={() => withdraw(k.fingerprint, k.label)}>
                    {m.sftp_withdraw()}
                  </DetailButton>
                </li>
              {/each}
            </ul>
          </div>
        {/each}

        <CommandBlock
          commands={status.commands}
          title={m.sftp_setupTitle()}
          intro={m.sftp_setupIntro({ path: status.staged_path })}
        />
      {/if}
    </section>
  </div>
</DetailPanel>
