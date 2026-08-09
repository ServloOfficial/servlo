<!--
  The account section of the site's database card. It is its own component
  because rotating a credential is a whole flow of its own, but it renders
  inside SiteDatabaseCard rather than beside it: which connection a site is on
  and which account it reaches it as are one subject, and two cards for two
  fields is the clutter CLAUDE.md warns about.
-->
<script lang="ts">
  import { loadSiteDBUser, rotateSiteDBUser, type SiteDBUser } from '$stores/dbUser';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let account = $state<SiteDBUser | null>(null);
  let loading = $state(true);
  // Two presses. Rotating changes a credential a running application is holding,
  // and the second press is where the operator reads what happens next.
  let confirming = $state(false);
  let rotating = $state(false);
  let rotated = $state('');
  let error = $state('');

  async function load() {
    loading = true;
    account = await loadSiteDBUser(site.domain);
    loading = false;
  }

  $effect(() => {
    void site.domain;
    void load();
  });

  const keys = $derived((account?.env_keys ?? []).join(', '));

  async function rotate() {
    confirming = false;
    rotating = true;
    rotated = '';
    error = '';
    const res = await rotateSiteDBUser(site.domain);
    rotating = false;
    if (res.ok) {
      rotated = m.dbuser_rotated({ keys: (res.env_keys ?? []).join(', ') });
      await load();
    } else {
      error = res.error || m.common_failed();
    }
  }
</script>

{#if !loading && account && !account.error}
  <div class="mt-5 pt-4 border-t border-gray-100 dark:border-servlo-border">
    <div class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.dbuser_title()}</div>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{m.dbuser_desc()}</p>

    <div class="mt-3 flex items-baseline gap-2">
      <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{m.dbuser_account()}</span>
      <span class="font-mono text-xs text-gray-800 dark:text-gray-100">{account.user}</span>
      <span class="text-[11px] text-gray-400 dark:text-gray-500">
        {m.dbuser_on({ location: account.location })}
      </span>
    </div>
    <p class="mt-1 text-[11px] leading-relaxed {account.own_account
      ? 'text-gray-400 dark:text-gray-500'
      : 'text-amber-600 dark:text-amber-400'}">
      {account.own_account ? m.dbuser_ownAccount() : m.dbuser_adminFallback()}
      {#if !account.own_account}
        {m.dbuser_adminFallbackHint()}
      {/if}
    </p>

    <p class="mt-3 text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">
      {keys ? m.dbuser_rotateHint({ keys }) : m.dbuser_rotateHintNoKeys()}
    </p>
    {#if account.note}
      <p class="mt-1 text-[11px] text-amber-600 dark:text-amber-400 leading-relaxed">{account.note}</p>
    {/if}

    <div class="mt-3 flex items-center gap-3">
      {#if confirming}
        <button
          type="button"
          onclick={rotate}
          disabled={rotating}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-servlo-red hover:bg-servlo-redhov text-white disabled:opacity-50 transition-colors"
        >
          {m.dbuser_confirmTitle()}
        </button>
        <button
          type="button"
          onclick={() => (confirming = false)}
          class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
        >
          {m.common_cancel()}
        </button>
      {:else}
        <button
          type="button"
          onclick={() => (confirming = true)}
          disabled={rotating}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
        >
          {rotating ? m.dbuser_rotating() : m.dbuser_rotate()}
        </button>
      {/if}
    </div>
    {#if confirming}
      <p class="mt-2 text-[11px] text-gray-500 dark:text-gray-400 leading-relaxed">
        {m.dbuser_confirmBody()}
      </p>
    {/if}

    {#if rotated}
      <p class="mt-3 text-xs text-green-600 dark:text-green-400 leading-relaxed">{rotated}</p>
    {/if}
    {#if error}
      <p class="mt-3 text-xs text-servlo-red whitespace-pre-wrap leading-relaxed">{error}</p>
    {/if}
  </div>
{/if}
