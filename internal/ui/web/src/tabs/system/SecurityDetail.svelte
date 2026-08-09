<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Icon from '$components/Icon.svelte';
  import CommandBlock from '$components/CommandBlock.svelte';
  import { security, securityLoaded, loadSecurity, addKey, removeKey } from '$stores/security';
  import { onMount } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  // The order on this page is the point. Everything above the keys is something
  // servlo can only report, with the command to run; the keys at the bottom are
  // the one thing on this screen servlo actually changes. Mixing them would
  // leave an operator clicking buttons that quietly do nothing.

  let key = $state('');
  let name = $state('');
  let busy = $state(false);
  let error = $state('');
  let removing = $state('');

  const s = $derived($security);
  const bad = $derived(s.findings.filter((f) => f.severity === 'bad').length);
  const warn = $derived(s.findings.filter((f) => f.severity === 'warn').length);

  // The audit summarises the same facts the sections below spell out, so the
  // page has to decide once who says what or it prints the ufw plan twice with
  // two Copy buttons twenty lines apart.
  //
  // The sections own the commands. A finding keeps its own block only for the
  // commands no section below repeats, which is how unattended-upgrades and a
  // credential with the wrong mode still get theirs.
  const shownBelow = $derived(
    new Set([...s.firewall.commands, ...(s.fail2ban.install_commands ?? []), s.fail2ban.status_command])
  );
  const remainingFix = (fix?: string[]) => (fix ?? []).filter((c) => !shownBelow.has(c));

  // The provider finding is the provider section, word for word. One of them.
  const findings = $derived(
    s.findings.filter((f) => !(s.provider.detail && f.detail?.startsWith(s.provider.detail)))
  );

  onMount(() => {
    void loadSecurity();
  });

  async function authorise() {
    busy = true;
    error = (await addKey(key.trim(), name.trim())) ?? '';
    busy = false;
    if (!error) {
      key = '';
      name = '';
    }
  }

  async function withdraw(fingerprint: string) {
    removing = fingerprint;
    error = (await removeKey(fingerprint)) ?? '';
    removing = '';
  }

  const tone = (severity: string) =>
    severity === 'bad' ? 'error' : severity === 'warn' ? 'warn' : 'ok';

  // Spelled out rather than indexed by the severity string, so a severity the
  // server adds later fails the type check here instead of rendering blank.
  const severityLabel = (severity: string) =>
    severity === 'bad' ? m.security_sev_bad() : severity === 'warn' ? m.security_sev_warn() : m.security_sev_ok();
</script>

{#snippet verdict()}
  {#if !$securityLoaded}
    <span class="text-xs text-gray-400">{m.common_loading()}</span>
  {:else if bad > 0}
    <StatusPill tone="error" label={m.security_pillToFix({ count: bad })} />
  {:else if warn > 0}
    <StatusPill tone="warn" label={m.security_pillToLookAt({ count: warn })} />
  {:else}
    <StatusPill tone="ok" label={m.security_pillClean()} />
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.security_title()} trailing={verdict} />

  <div class="flex-1 min-h-0 overflow-y-auto">
    <p class="px-3 sm:px-5 pt-3 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
      {m.security_intro()}
    </p>

    <section class="px-3 sm:px-5 py-4 space-y-2">
      <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{m.security_auditHeading()}</h3>
      {#if !$securityLoaded}
        <p class="text-xs text-gray-400">{m.common_loading()}</p>
      {:else}
        {#each findings as finding (finding.title)}
          <div class="rounded-sm border border-gray-200 dark:border-servlo-border px-3 py-2 space-y-2">
            <div class="flex items-start gap-2">
              <span class="mt-0.5 shrink-0">
                <StatusPill tone={tone(finding.severity)} label={severityLabel(finding.severity)} />
              </span>
              <div class="min-w-0 space-y-1">
                <p class="text-xs font-medium text-gray-900 dark:text-gray-100">{finding.title}</p>
                {#if finding.detail}
                  <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{finding.detail}</p>
                {/if}
              </div>
            </div>
            <CommandBlock commands={remainingFix(finding.fix)} />
          </div>
        {/each}
      {/if}
    </section>

    <section class="px-3 sm:px-5 pb-4 space-y-2">
      <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{m.security_firewallHeading()}</h3>
      <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{s.firewall.why}</p>
      {#if s.firewall.open.length > 0}
        <p class="text-[11px] text-gray-500 dark:text-gray-400">
          {m.security_openPorts({ ports: s.firewall.open.join(', ') })}
        </p>
      {/if}
      {#if s.firewall.unexpected.length > 0}
        <div
          class="flex items-start gap-2 px-3 py-2 rounded-sm bg-amber-50 dark:bg-amber-900/15 border border-amber-200 dark:border-amber-900/40"
        >
          <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
          <p class="text-xs text-amber-700 dark:text-amber-300 leading-relaxed">
            {m.security_unexpectedPorts({ ports: s.firewall.unexpected.join(', ') })}
          </p>
        </div>
      {/if}
      <CommandBlock commands={s.firewall.commands} />
      <p class="text-[10px] text-gray-400 dark:text-gray-600">
        {m.security_sshPortNote({ port: s.ssh_port })}
      </p>
    </section>

    <section class="px-3 sm:px-5 pb-4 space-y-2">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{m.security_fail2banHeading()}</h3>
        <StatusPill
          tone={s.fail2ban.running ? 'ok' : 'warn'}
          label={s.fail2ban.running ? m.security_fail2banRunning() : m.security_fail2banStopped()}
        />
      </div>
      <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{s.fail2ban.why}</p>
      <CommandBlock
        commands={s.fail2ban.running
          ? [s.fail2ban.status_command, m.security_unbanCommand()]
          : [...(s.fail2ban.install_commands ?? []), s.fail2ban.status_command]}
      />
    </section>

    {#if s.provider.detail}
      <section class="px-3 sm:px-5 pb-4 space-y-2">
        <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">
          {s.provider.name || m.security_cloudFirewallHeading()}
        </h3>
        <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{s.provider.detail}</p>
        {#if s.provider.firewall_url}
          <a
            href={s.provider.firewall_url}
            target="_blank"
            rel="noreferrer noopener"
            class="inline-block text-[11px] text-servlo-red hover:underline">{s.provider.firewall_url}</a
          >
        {/if}
      </section>
    {/if}

    <!-- The one thing on this page servlo changes itself. -->
    <section class="px-3 sm:px-5 pb-4 space-y-3 border-t border-gray-100 dark:border-servlo-border pt-4">
      <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{m.security_keysHeading()}</h3>
      <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">
        {m.security_keysIntro({ path: s.keys_path })}
      </p>

      <div class="max-w-sm">
        <label class="block space-y-1">
          <span class="block text-[11px] text-gray-500 dark:text-gray-400">{m.security_keyName()}</span>
          <input
            type="text"
            bind:value={name}
            disabled={busy}
            autocomplete="off"
            spellcheck="false"
            class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-xs font-mono text-gray-900 dark:text-gray-100 disabled:opacity-50"
          />
          <span class="block text-[10px] text-gray-400 dark:text-gray-600">{m.security_keyNameHint()}</span>
        </label>
      </div>
      <label class="block space-y-1">
        <span class="block text-[11px] text-gray-500 dark:text-gray-400">{m.security_publicKey()}</span>
        <textarea
          bind:value={key}
          disabled={busy}
          rows="3"
          spellcheck="false"
          class="w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border bg-white dark:bg-black/30 text-[11px] font-mono text-gray-900 dark:text-gray-100 disabled:opacity-50"
        ></textarea>
        <span class="block text-[10px] text-gray-400 dark:text-gray-600">{m.security_publicKeyHint()}</span>
      </label>
      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
      <div class="flex justify-end">
        <DetailButton
          tone="primary"
          onclick={authorise}
          loading={busy}
          disabled={busy || !key.trim() || !name.trim()}
        >
          {busy ? m.security_authorising() : m.security_authorise()}
        </DetailButton>
      </div>

      {#if s.keys.length === 0}
        <EmptyState title={m.security_noKeys()} />
      {:else}
        <ul class="rounded-sm border border-gray-200 dark:border-servlo-border divide-y divide-gray-100 dark:divide-servlo-border">
          {#each s.keys as k (k.fingerprint)}
            <li class="flex items-center justify-between gap-3 px-3 py-2">
              <div class="min-w-0">
                <p class="text-xs text-gray-800 dark:text-gray-100 truncate">{k.comment}</p>
                <p class="text-[10px] font-mono text-gray-400 dark:text-gray-600 truncate">
                  {k.type} {k.fingerprint}
                </p>
              </div>
              <DetailButton
                tone="danger"
                loading={removing === k.fingerprint}
                disabled={removing === k.fingerprint}
                onclick={() => withdraw(k.fingerprint)}
              >
                {m.security_removeKey()}
              </DetailButton>
            </li>
          {/each}
        </ul>
      {/if}

      <!-- Said on the page, not only in the docs. Somebody adding keys here is
           exactly the person who might assume passwords have been turned off. -->
      <div
        class="flex items-start gap-2 px-3 py-2 rounded-sm bg-gray-50 dark:bg-white/3 border border-gray-200 dark:border-servlo-border"
      >
        <Icon name="alert" class="w-3.5 h-3.5 mt-0.5 shrink-0 text-gray-400 dark:text-gray-500" />
        <p class="text-xs text-gray-600 dark:text-gray-300 leading-relaxed">{m.security_passwordsStayOn()}</p>
      </div>
    </section>
  </div>
</DetailPanel>
