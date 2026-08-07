<script lang="ts">
  import { session, signIn, createFirstAccount } from '$stores/session';

  // The same form does both jobs. Creating the first account and signing in
  // ask for the same two things, and a separate screen for each would be two
  // screens the operator has to tell apart before they can type.
  let username = $state('');
  let password = $state('');
  let confirm = $state('');
  let code = $state('');

  const setup = $derived($state.snapshot($session).setupNeeded);
  const busy = $derived($session.busy);
  const mismatch = $derived(setup && confirm !== '' && password !== confirm);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy || mismatch) return;
    if (setup) {
      await createFirstAccount(username, password);
      return;
    }
    const ok = await signIn(username, password, code);
    if (ok) {
      password = '';
      code = '';
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center px-4 bg-gray-50 dark:bg-black">
  <form
    onsubmit={submit}
    class="w-full max-w-sm rounded-2xl border border-gray-200 dark:border-servlo-border bg-white dark:bg-servlo-card p-6 shadow-sm"
  >
    <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
      {setup ? 'Set up the Servlo panel' : 'Sign in to Servlo'}
    </h1>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
      {setup
        ? 'This panel has no account yet. The first one is an administrator.'
        : 'This panel is on the internet. Sign in to continue.'}
    </p>

    <label class="block mt-5 text-xs font-medium text-gray-600 dark:text-gray-400" for="login-username">
      Username
    </label>
    <input
      id="login-username"
      bind:value={username}
      autocomplete="username"
      autocapitalize="none"
      spellcheck="false"
      required
      class="mt-1 w-full rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 focus:outline-hidden focus:ring-2 focus:ring-servlo-red/40"
    />

    <label class="block mt-4 text-xs font-medium text-gray-600 dark:text-gray-400" for="login-password">
      Password
    </label>
    <input
      id="login-password"
      type="password"
      bind:value={password}
      autocomplete={setup ? 'new-password' : 'current-password'}
      required
      class="mt-1 w-full rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 focus:outline-hidden focus:ring-2 focus:ring-servlo-red/40"
    />

    {#if setup}
      <label class="block mt-4 text-xs font-medium text-gray-600 dark:text-gray-400" for="login-confirm">
        Confirm password
      </label>
      <input
        id="login-confirm"
        type="password"
        bind:value={confirm}
        autocomplete="new-password"
        required
        class="mt-1 w-full rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 focus:outline-hidden focus:ring-2 focus:ring-servlo-red/40"
      />
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        At least 12 characters. A few unrelated words are easier to remember and harder to guess
        than a short one with symbols in it.
      </p>
      {#if mismatch}
        <p class="mt-2 text-xs text-red-500" data-login-error>The two passwords do not match.</p>
      {/if}
    {/if}

    {#if $session.codeRequired && !setup}
      <label class="block mt-4 text-xs font-medium text-gray-600 dark:text-gray-400" for="login-code">
        Code
      </label>
      <input
        id="login-code"
        bind:value={code}
        inputmode="text"
        autocomplete="one-time-code"
        autocapitalize="none"
        spellcheck="false"
        data-login-code
        class="mt-1 w-full rounded-lg border border-gray-200 dark:border-servlo-border bg-white dark:bg-black/40 px-3 py-2 text-sm font-mono text-gray-900 dark:text-gray-100 focus:outline-hidden focus:ring-2 focus:ring-servlo-red/40"
      />
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        From your authenticator app, or one of the recovery codes you wrote down.
      </p>
    {/if}

    {#if $session.error}
      <p class="mt-4 text-xs text-red-500" data-login-error>{$session.error}</p>
    {/if}

    <button
      type="submit"
      disabled={busy || mismatch}
      class="mt-5 w-full rounded-lg bg-servlo-red hover:bg-servlo-redhov disabled:opacity-50 px-3 py-2 text-sm font-medium text-white transition-colors"
    >
      {#if busy}
        Working…
      {:else}
        {setup ? 'Create the account' : 'Sign in'}
      {/if}
    </button>

    {#if !setup}
      <p class="mt-4 text-[11px] leading-relaxed text-gray-400 dark:text-gray-500">
        Locked out? A shell on the server can reset it:
        <code class="font-mono">servlo users password &lt;name&gt;</code>
      </p>
    {/if}
  </form>
</div>
