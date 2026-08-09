<script lang="ts">
  import { blankForm, formFrom, type SMTPForm, type SMTPResult, type SMTPStatus } from '$stores/smtp';
  import { m } from '../paraglide/messages.js';

  // The mail form, shared by the site card and the panel's own settings. They
  // are the same seven fields against two endpoints, so the caller passes the
  // three actions and this owns the fields, the validation and the states.

  interface Props {
    status: SMTPStatus;
    /** Disables the fields while the caller is still loading. */
    loading?: boolean;
    /** Placeholder for the test recipient, and where a blank test goes. */
    defaultRecipient?: string;
    onsave: (form: SMTPForm) => Promise<SMTPResult>;
    ontest: (to: string) => Promise<SMTPResult>;
    onremove: () => Promise<SMTPResult>;
    onchanged?: () => void;
  }
  let { status, loading = false, defaultRecipient = '', onsave, ontest, onremove, onchanged }:
    Props = $props();

  let form = $state<SMTPForm>(blankForm());
  let recipient = $state('');
  let saving = $state(false);
  let testing = $state(false);
  let removing = $state(false);
  let confirmingRemove = $state(false);
  let notice = $state('');
  let warning = $state('');
  let error = $state('');

  // Re-seed whenever the caller hands over a different account, which on the
  // site card is every time the operator picks another site.
  $effect(() => {
    form = formFrom(status.settings);
    recipient = '';
    notice = '';
    warning = '';
    error = '';
    confirmingRemove = false;
  });

  const busy = $derived(saving || testing || removing || loading);
  const hasPassword = $derived(Boolean(status.settings.has_password));
  const canSend = $derived(status.configured && !busy);

  function clear() {
    notice = '';
    warning = '';
    error = '';
  }

  async function save() {
    clear();
    saving = true;
    const res = await onsave({ ...form, host: form.host.trim() });
    saving = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    warning = res.warning ?? '';
    notice = res.env_keys?.length
      ? m.smtp_savedWithKeys({ keys: res.env_keys.join(', ') })
      : m.smtp_saved();
    onchanged?.();
  }

  async function test() {
    clear();
    testing = true;
    const res = await ontest(recipient.trim());
    testing = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    notice = m.smtp_testSent({ to: res.to ?? recipient.trim() });
  }

  async function remove() {
    clear();
    confirmingRemove = false;
    removing = true;
    const res = await onremove();
    removing = false;
    if (!res.ok) {
      error = res.error || m.common_failed();
      return;
    }
    notice = m.smtp_removed();
    onchanged?.();
  }

  const field =
    'w-full px-2 py-1.5 rounded-sm border border-gray-300 dark:border-servlo-border ' +
    'bg-white dark:bg-black/30 text-xs text-gray-900 dark:text-gray-100 disabled:opacity-50';
  const labelText = 'block text-[11px] text-gray-500 dark:text-gray-400';
  const hint = 'block text-[10px] text-gray-400 dark:text-gray-600';
</script>

<div class="space-y-3">
  <div class="grid gap-3 sm:grid-cols-2">
    <label class="space-y-1 sm:col-span-2">
      <span class={labelText}>{m.smtp_host()}</span>
      <input
        type="text"
        bind:value={form.host}
        disabled={busy}
        autocomplete="off"
        spellcheck="false"
        placeholder="smtp.postmarkapp.com"
        class="{field} font-mono"
      />
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_port()}</span>
      <input type="number" min="1" max="65535" bind:value={form.port} disabled={busy} class={field} />
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_encryption()}</span>
      <select bind:value={form.encryption} disabled={busy} class={field}>
        <option value="starttls">{m.smtp_encryption_starttls()}</option>
        <option value="tls">{m.smtp_encryption_tls()}</option>
        <option value="none">{m.smtp_encryption_none()}</option>
      </select>
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_username()}</span>
      <input
        type="text"
        bind:value={form.username}
        disabled={busy}
        autocomplete="off"
        spellcheck="false"
        class="{field} font-mono"
      />
      <span class={hint}>{m.smtp_usernameHint()}</span>
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_password()}</span>
      <input
        type="password"
        bind:value={form.password}
        disabled={busy}
        autocomplete="new-password"
        placeholder={hasPassword ? m.smtp_passwordKept() : ''}
        class="{field} font-mono"
      />
      <span class={hint}>{hasPassword ? m.smtp_passwordKeptHint() : m.smtp_passwordHint()}</span>
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_fromAddress()}</span>
      <input
        type="email"
        bind:value={form.from_address}
        disabled={busy}
        autocomplete="off"
        spellcheck="false"
        placeholder="hello@example.com"
        class="{field} font-mono"
      />
      <span class={hint}>{m.smtp_fromAddressHint()}</span>
    </label>

    <label class="space-y-1">
      <span class={labelText}>{m.smtp_fromName()}</span>
      <input type="text" bind:value={form.from_name} disabled={busy} autocomplete="off" class={field} />
    </label>
  </div>

  <div class="flex flex-wrap items-center gap-3 pt-1">
    <button
      type="button"
      onclick={save}
      disabled={busy}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
    >
      {saving ? m.smtp_saving() : m.smtp_save()}
    </button>

    {#if status.configured}
      <button
        type="button"
        onclick={test}
        disabled={!canSend}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
      >
        {testing ? m.smtp_testing() : m.smtp_test()}
      </button>
      <input
        type="email"
        bind:value={recipient}
        disabled={busy}
        autocomplete="off"
        spellcheck="false"
        placeholder={defaultRecipient || m.smtp_testRecipient()}
        class="{field} font-mono max-w-[16rem]"
      />
    {/if}
  </div>

  {#if status.configured}
    <div class="flex items-center gap-3">
      {#if confirmingRemove}
        <button
          type="button"
          onclick={remove}
          disabled={busy}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
        >
          {m.smtp_removeConfirm()}
        </button>
        <button
          type="button"
          onclick={() => (confirmingRemove = false)}
          class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
        >
          {m.common_cancel()}
        </button>
      {:else}
        <button
          type="button"
          onclick={() => (confirmingRemove = true)}
          disabled={busy}
          class="text-xs text-gray-500 hover:text-servlo-red disabled:opacity-50 transition-colors"
        >
          {removing ? m.smtp_removing() : m.smtp_remove()}
        </button>
      {/if}
    </div>
    {#if confirmingRemove}
      <p class="text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">{m.smtp_removeBody()}</p>
    {/if}
  {/if}

  {#if notice}
    <p class="text-xs text-green-600 dark:text-green-400 leading-relaxed">{notice}</p>
  {/if}
  {#if warning}
    <p class="text-xs text-amber-600 dark:text-amber-400 leading-relaxed">{warning}</p>
  {/if}
  {#if error}
    <p class="text-xs text-servlo-red whitespace-pre-wrap leading-relaxed">{error}</p>
  {/if}
</div>
