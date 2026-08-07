<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { session, loadSession } from '$stores/session';
  import { apiFetch } from '$lib/api';

  // Enrolment happens here rather than on a wizard of its own, because it is
  // three steps and each one is a sentence: scan this, type what it shows,
  // write these down.
  type Stage = 'idle' | 'enrolling' | 'codes' | 'disabling';

  let stage = $state<Stage>('idle');
  let secret = $state('');
  let uri = $state('');
  let code = $state('');
  let password = $state('');
  let recoveryCodes = $state<string[]>([]);
  let error = $state('');
  let busy = $state(false);

  // The QR is drawn from the otpauth URI by an off-the-shelf encoder on the
  // server, so the browser renders an image rather than carrying a QR library
  // it needs on one screen.
  const qrSrc = $derived(uri ? '/api/auth/totp/qr?uri=' + encodeURIComponent(uri) : '');

  async function start() {
    busy = true;
    error = '';
    try {
      const res = await apiFetch('/api/auth/totp/enrol', { method: 'POST' });
      if (!res.ok) throw new Error((await res.text()).trim());
      const data = (await res.json()) as { secret: string; uri: string };
      secret = data.secret;
      uri = data.uri;
      stage = 'enrolling';
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not start enrolment.';
    }
    busy = false;
  }

  async function confirm() {
    busy = true;
    error = '';
    try {
      const res = await apiFetch('/api/auth/totp/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ secret, code })
      });
      if (!res.ok) throw new Error((await res.text()).trim());
      const data = (await res.json()) as { recovery_codes: string[] };
      recoveryCodes = data.recovery_codes;
      stage = 'codes';
      secret = '';
      uri = '';
      code = '';
      await loadSession();
    } catch (e) {
      error = e instanceof Error ? e.message : 'That code did not match.';
    }
    busy = false;
  }

  async function disable() {
    busy = true;
    error = '';
    try {
      const res = await apiFetch('/api/auth/totp/disable', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });
      if (!res.ok) throw new Error((await res.text()).trim());
      password = '';
      stage = 'idle';
      await loadSession();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not turn it off.';
    }
    busy = false;
  }

  function cancel() {
    stage = 'idle';
    secret = '';
    uri = '';
    code = '';
    password = '';
    error = '';
  }
</script>

<SettingsCard>
  <div class="flex items-center justify-between gap-3 mb-2">
    <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Two-factor authentication</span>
    <span
      data-totp-state
      class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$session.totpEnabled
        ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
        : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}"
    >
      <span class="w-1.5 h-1.5 rounded-full {$session.totpEnabled ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
      {$session.totpEnabled ? 'on' : 'off'}
    </span>
  </div>

  <p class="text-xs text-gray-500 dark:text-gray-400">
    A code from your phone on top of the password, so a stolen password is not enough on its own.
  </p>

  {#if stage === 'idle'}
    {#if $session.totpEnabled}
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {$session.recoveryCodesLeft} recovery code{$session.recoveryCodesLeft === 1 ? '' : 's'} left.
      </p>
      <button
        onclick={() => (stage = 'disabling')}
        class="mt-3 rounded-lg px-3 py-1.5 text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 transition-colors"
      >
        Turn off
      </button>
    {:else}
      <button
        onclick={start}
        disabled={busy}
        class="mt-3 rounded-lg px-3 py-1.5 text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
      >
        Turn on
      </button>
    {/if}
  {/if}

  {#if stage === 'enrolling'}
    <div class="mt-4">
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">Scan this with your authenticator app:</p>
      {#if qrSrc}
        <img src={qrSrc} alt="Enrolment QR code" width="180" height="180" class="rounded-lg bg-white p-2" />
      {/if}
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        Or type it in: <code class="font-mono break-all">{secret}</code>
      </p>

      <label class="block mt-4 text-xs font-medium text-gray-600 dark:text-gray-400" for="totp-confirm">
        Then enter the code it shows
      </label>
      <input
        id="totp-confirm"
        bind:value={code}
        autocomplete="one-time-code"
        inputmode="numeric"
        class="mt-1 w-40 rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm font-mono text-gray-900 dark:text-gray-100"
      />
      <div class="mt-3 flex items-center gap-2">
        <button
          onclick={confirm}
          disabled={busy || code === ''}
          class="rounded-lg bg-servlo-red hover:bg-servlo-redhov disabled:opacity-50 px-3 py-1.5 text-sm font-medium text-white transition-colors"
        >
          Confirm
        </button>
        <button onclick={cancel} class="text-sm text-gray-500 dark:text-gray-400 hover:underline">Cancel</button>
      </div>
    </div>
  {/if}

  {#if stage === 'codes'}
    <div class="mt-4">
      <p class="text-xs text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2">
        Write these down now. Each works once, in place of a code from the app, and this is the only
        time they are shown.
      </p>
      <ul class="mt-3 grid grid-cols-2 gap-1 font-mono text-sm text-gray-800 dark:text-gray-200" data-recovery-codes>
        {#each recoveryCodes as recoveryCode (recoveryCode)}
          <li>{recoveryCode}</li>
        {/each}
      </ul>
      <button
        onclick={() => {
          recoveryCodes = [];
          stage = 'idle';
        }}
        class="mt-3 rounded-lg px-3 py-1.5 text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 transition-colors"
      >
        I have written them down
      </button>
    </div>
  {/if}

  {#if stage === 'disabling'}
    <div class="mt-4">
      <label class="block text-xs font-medium text-gray-600 dark:text-gray-400" for="totp-password">
        Confirm your password to turn it off
      </label>
      <input
        id="totp-password"
        type="password"
        bind:value={password}
        autocomplete="current-password"
        class="mt-1 w-full rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm text-gray-900 dark:text-gray-100"
      />
      <div class="mt-3 flex items-center gap-2">
        <button
          onclick={disable}
          disabled={busy || password === ''}
          class="rounded-lg bg-servlo-red hover:bg-servlo-redhov disabled:opacity-50 px-3 py-1.5 text-sm font-medium text-white transition-colors"
        >
          Turn off
        </button>
        <button onclick={cancel} class="text-sm text-gray-500 dark:text-gray-400 hover:underline">Cancel</button>
      </div>
    </div>
  {/if}

  {#if error}
    <p class="mt-3 text-xs text-red-500" data-totp-error>{error}</p>
  {/if}
</SettingsCard>
