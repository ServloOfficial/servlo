<script lang="ts">
  import DetailButton from '$components/DetailButton.svelte';
  import SegmentedControl, { type SegmentOption } from '$components/SegmentedControl.svelte';
  import SettingsField from '$components/SettingsField.svelte';
  import { addConnection, defaultPortFor } from '$stores/dbConnections';
  import { m } from '../../paraglide/messages.js';

  // The two kinds of connection differ in every field but the name, so they are
  // one form with a mode rather than two forms sharing a heading.
  interface Props {
    services: string[];
    onadded: () => void;
    oncancel: () => void;
  }
  let { services, onadded, oncancel }: Props = $props();

  type Mode = 'local' | 'managed';

  let mode = $state<Mode>('local');
  let name = $state('');
  let service = $state(services[0] ?? '');
  let engine = $state('mysql');
  let host = $state('');
  let port = $state<number | null>(null);
  let user = $state('');
  let password = $state('');
  let tlsMode = $state('');
  let caCert = $state('');
  let submitting = $state(false);
  let error = $state('');

  const modes: SegmentOption<Mode>[] = [
    { value: 'local', label: m.dbconn_modeLocal() },
    { value: 'managed', label: m.dbconn_modeManaged() }
  ];

  const field =
    'w-full px-2 py-1 rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card text-xs text-gray-700 dark:text-gray-200';

  const filled = (v: string) => v.trim() !== '';

  const complete = $derived(
    filled(name) &&
      (mode === 'local'
        ? filled(service)
        : filled(engine) && filled(host) && filled(user) && filled(password))
  );

  async function submit() {
    if (!complete || submitting) return;
    submitting = true;
    error = '';
    const res = await addConnection(
      mode === 'local'
        ? { name: name.trim(), service }
        : {
            name: name.trim(),
            engine,
            host: host.trim(),
            // An untouched box means the engine's usual port, which the server
            // fills in; sending zero would be asking for port zero.
            port: port == null || !Number.isFinite(port) ? defaultPortFor(engine) : Math.trunc(port),
            user: user.trim(),
            password,
            tls_mode: tlsMode,
            ca_cert: caCert.trim()
          }
    );
    submitting = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    onadded();
  }
</script>

<div class="mt-4 pt-4 border-t border-gray-200 dark:border-servlo-border space-y-3">
  <h3 class="text-[11px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">
    {m.dbconn_addTitle()}
  </h3>

  <!-- The card is as wide as the page; the form is not, so a twenty-character
       name is not asked for in a box the width of the window. -->
  <div class="max-w-3xl space-y-4">
    <SegmentedControl
      options={modes}
      value={mode}
      label={m.dbconn_mode()}
      onchange={(v) => (mode = v)}
    />

    {#if mode === 'managed'}
      <p class="text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
        {m.dbconn_managedNote()}
      </p>
    {/if}

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <SettingsField label={m.dbconn_name()} hint={m.dbconn_nameHint()} forId="dbconn-name">
        <input
          id="dbconn-name"
          type="text"
          autocomplete="off"
          placeholder={m.dbconn_namePlaceholder()}
          bind:value={name}
          class={field}
        />
      </SettingsField>

      {#if mode === 'local'}
        <SettingsField label={m.dbconn_service()} hint={m.dbconn_serviceHint()} forId="dbconn-service">
          {#if services.length === 0}
            <p class="text-[11px] text-gray-400 dark:text-gray-500">{m.dbconn_noServices()}</p>
          {:else}
            <select id="dbconn-service" bind:value={service} class={field}>
              {#each services as s (s)}
                <option value={s}>{s}</option>
              {/each}
            </select>
          {/if}
        </SettingsField>
      {:else}
        <SettingsField label={m.dbconn_engine()} forId="dbconn-engine">
          <select id="dbconn-engine" bind:value={engine} class={field}>
            <option value="mysql">{m.dbconn_engineMysql()}</option>
            <option value="postgres">{m.dbconn_enginePostgres()}</option>
          </select>
        </SettingsField>
      {/if}
    </div>

    {#if mode === 'managed'}
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div class="sm:col-span-2">
          <SettingsField label={m.dbconn_host()} forId="dbconn-host">
            <input
              id="dbconn-host"
              type="text"
              autocomplete="off"
              placeholder={m.dbconn_hostPlaceholder()}
              bind:value={host}
              class={field}
            />
          </SettingsField>
        </div>
        <SettingsField label={m.dbconn_port()} forId="dbconn-port">
          <input
            id="dbconn-port"
            type="number"
            min="1"
            max="65535"
            placeholder={String(defaultPortFor(engine))}
            bind:value={port}
            class={field}
          />
        </SettingsField>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <SettingsField label={m.dbconn_user()} forId="dbconn-user">
          <input id="dbconn-user" type="text" autocomplete="off" bind:value={user} class={field} />
        </SettingsField>
        <SettingsField label={m.dbconn_password()} hint={m.dbconn_passwordHint()} forId="dbconn-password">
          <input
            id="dbconn-password"
            type="password"
            autocomplete="new-password"
            bind:value={password}
            class={field}
          />
        </SettingsField>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <SettingsField label={m.dbconn_tls()} forId="dbconn-tls">
          <select id="dbconn-tls" bind:value={tlsMode} class={field}>
            <option value="">{m.dbconn_tlsOff()}</option>
            <option value="require">{m.dbconn_tlsRequire()}</option>
            <option value="verify-ca">{m.dbconn_tlsVerifyCA()}</option>
          </select>
        </SettingsField>
        <SettingsField label={m.dbconn_caCert()} hint={m.dbconn_caCertHint()} forId="dbconn-ca">
          <input
            id="dbconn-ca"
            type="text"
            autocomplete="off"
            placeholder={m.dbconn_caCertPlaceholder()}
            bind:value={caCert}
            class={field}
          />
        </SettingsField>
      </div>
    {/if}

    <div class="flex items-center gap-2">
      <DetailButton tone="primary" onclick={submit} disabled={!complete || submitting}>
        {submitting ? m.dbconn_adding() : m.dbconn_add()}
      </DetailButton>
      <DetailButton tone="secondary" onclick={oncancel}>{m.common_cancel()}</DetailButton>
    </div>

    {#if error}
      <p class="text-xs text-servlo-red whitespace-pre-wrap">{error}</p>
    {/if}
  </div>
</div>
