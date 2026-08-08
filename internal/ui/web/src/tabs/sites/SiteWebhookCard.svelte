<script lang="ts">
  import SettingsCard from '$components/SettingsCard.svelte';
  import { loadSiteWebhook, saveSiteWebhook, type SiteWebhook } from '$stores/deploy';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let hook = $state<SiteWebhook | null>(null);
  let branch = $state('');
  // Held only for as long as this page is open. The server sends it once, on
  // the response that mints it, and will not send it again.
  let freshSecret = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let copied = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await loadSiteWebhook(site.domain);
      hook = res;
      branch = res.branch ?? '';
    } catch {
      error = m.sites_webhook_loadFailed();
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  async function save(values: { enabled: boolean; regenerate?: boolean }) {
    saving = true;
    error = '';
    const res = await saveSiteWebhook(site.domain, { branch: branch.trim(), ...values });
    saving = false;
    if (res.error) {
      error = res.error;
      return;
    }
    hook = res;
    branch = res.branch ?? '';
    if (res.secret) freshSecret = res.secret;
    if (!values.enabled) freshSecret = '';
  }

  // The full URL, because a path on its own is not something anyone can paste
  // into GitHub.
  const fullURL = $derived(hook?.url ? new URL(hook.url, location.origin).href : '');

  async function copy(what: string, value: string) {
    try {
      await navigator.clipboard.writeText(value);
      copied = what;
      setTimeout(() => (copied = ''), 1500);
    } catch {
      /* a browser that refuses the clipboard still shows the value */
    }
  }
</script>

<SettingsCard>
  <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
    {m.sites_webhook_title()}
  </h2>
  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
    {m.sites_webhook_desc()}
  </p>

  {#if loading}
    <p class="mt-4 text-xs text-gray-400">…</p>
  {:else if !hook?.enabled}
    <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{m.sites_webhook_off()}</p>
    <div class="mt-4">
      <button
        type="button"
        onclick={() => save({ enabled: true })}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_webhook_saving() : m.sites_webhook_enable()}
      </button>
    </div>
  {:else}
    <div class="mt-4 space-y-4">
      <div>
        <span class="block text-xs font-medium text-gray-700 dark:text-gray-200">
          {m.sites_webhook_url()}
        </span>
        <div class="mt-1 flex items-center gap-2">
          <code
            class="flex-1 min-w-0 truncate rounded-md border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900 px-2.5 py-1.5 font-mono text-xs text-gray-800 dark:text-gray-200"
            >{fullURL}</code
          >
          <button
            type="button"
            onclick={() => copy('url', fullURL)}
            class="shrink-0 text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
          >
            {copied === 'url' ? m.sites_webhook_copied() : m.sites_webhook_copy()}
          </button>
        </div>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{m.sites_webhook_urlHint()}</p>
      </div>

      <div>
        <span class="block text-xs font-medium text-gray-700 dark:text-gray-200">
          {m.sites_webhook_secret()}
        </span>
        {#if freshSecret}
          <div class="mt-1 flex items-center gap-2">
            <code
              class="flex-1 min-w-0 truncate rounded-md border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950 px-2.5 py-1.5 font-mono text-xs text-gray-800 dark:text-gray-200"
              >{freshSecret}</code
            >
            <button
              type="button"
              onclick={() => copy('secret', freshSecret)}
              class="shrink-0 text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
            >
              {copied === 'secret' ? m.sites_webhook_copied() : m.sites_webhook_copy()}
            </button>
          </div>
          <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
            {m.sites_webhook_secretOnce()}
          </p>
        {:else}
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {m.sites_webhook_secretHidden()}
          </p>
        {/if}
      </div>

      <div>
        <label
          for="webhook-branch"
          class="block text-xs font-medium text-gray-700 dark:text-gray-200"
        >
          {m.sites_webhook_branch()}
        </label>
        <input
          id="webhook-branch"
          bind:value={branch}
          spellcheck="false"
          placeholder={m.sites_webhook_branchAny()}
          class="mt-1 w-full rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 px-2.5 py-1.5 font-mono text-xs text-gray-800 dark:text-gray-200 focus:outline-none focus:ring-1 focus:ring-servlo-red"
        />
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
          {m.sites_webhook_branchHint()}
        </p>
      </div>
    </div>

    <div class="mt-4 flex items-center gap-3 flex-wrap">
      <button
        type="button"
        onclick={() => save({ enabled: true })}
        disabled={saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
      >
        {saving ? m.sites_webhook_saving() : m.sites_webhook_save()}
      </button>
      <button
        type="button"
        onclick={() => save({ enabled: true, regenerate: true })}
        disabled={saving}
        class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 disabled:opacity-50 transition-colors"
        title={m.sites_webhook_regenerateHint()}
      >
        {m.sites_webhook_regenerate()}
      </button>
      <button
        type="button"
        onclick={() => save({ enabled: false })}
        disabled={saving}
        class="text-xs text-servlo-red hover:underline disabled:opacity-50 transition-colors"
      >
        {m.sites_webhook_disable()}
      </button>
    </div>
  {/if}

  {#if error}
    <p class="mt-3 text-xs text-servlo-red">{error}</p>
  {/if}
</SettingsCard>
