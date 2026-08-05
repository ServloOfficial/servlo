<script lang="ts">
  /**
   * The padlock beside a site's URL, and the DNS gate behind it.
   *
   * On an unsecured site the button stays disabled until every domain resolves
   * to this server, and the tooltip says exactly what does not match. That is
   * the point of the whole control: Let's Encrypt locks an account out of
   * retrying a domain after five failed validations, so a click against a
   * record that has not propagated costs an hour of waiting to learn what a DNS
   * lookup answers for free.
   *
   * While issuing, the tooltip follows the attempt's own progress log, because
   * securing a site is one long request and the panel cannot learn anything
   * from the response it is still waiting for.
   */
  import { onDestroy } from 'svelte';
  import { type Site, type TLSStatus, toggleTLS, tlsStatus, loadSites } from '$stores/sites';
  import { openErrorModal } from '$stores/modals';
  import { tooltip } from '$lib/tooltip';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    /** False on a paused site, where nothing about TLS can be changed. */
    toggleable?: boolean;
  }
  let { site, toggleable = true }: Props = $props();

  let busy = $state(false);
  let dns = $state<TLSStatus | null>(null);
  let progress = $state<string[]>([]);
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  const secured = $derived(Boolean(site.tls));
  // Only an unsecured site is gated: a secured one is already proven, and
  // blocking "turn HTTPS off" on a DNS check would strand a site whose domain
  // has moved away.
  const gated = $derived(!secured && dns !== null && !dns.ready);
  const disabled = $derived(busy || gated);

  const label = $derived(secured ? m.sites_controls_httpsToggle_on() : m.sites_controls_httpsToggle_off());
  const hint = $derived(
    busy && progress.length > 0
      ? progress[progress.length - 1]
      : gated
        ? (dns?.message ?? label)
        : label
  );

  async function refreshDNS() {
    if (secured) {
      dns = null;
      return;
    }
    try {
      dns = await tlsStatus(site.domain);
    } catch {
      // A status we cannot read is not a mismatch. Leaving the button enabled
      // lets the issuance itself report the real failure, which is better than
      // blocking on a check that did not answer.
      dns = null;
    }
  }

  // Re-checked whenever the site or its TLS state changes, so adding a domain
  // and coming back shows the new domain's records rather than a stale verdict.
  $effect(() => {
    void site.domain;
    void site.tls;
    refreshDNS();
  });

  function startPolling() {
    stopPolling();
    pollTimer = setInterval(async () => {
      try {
        const s = await tlsStatus(site.domain);
        progress = s.progress ?? [];
      } catch {
        // Ignored: a dropped poll costs one missing progress line.
      }
    }, 1500);
  }

  function stopPolling() {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = null;
  }

  onDestroy(stopPolling);

  async function flip() {
    if (disabled) return;
    busy = true;
    progress = [];
    if (!secured) startPolling();
    try {
      const res = await toggleTLS(site);
      if (!res.ok && res.error) openErrorModal(res.error);
      await loadSites();
      await refreshDNS();
    } finally {
      stopPolling();
      busy = false;
    }
  }
</script>

{#if toggleable}
  <button
    type="button"
    onclick={flip}
    {disabled}
    aria-label={label}
    data-tls-gated={gated ? 'true' : 'false'}
    use:tooltip={hint}
    class="shrink-0 -ml-1 p-1 rounded-sm transition-colors disabled:opacity-50 {secured
      ? 'text-emerald-500 hover:text-emerald-600 hover:bg-emerald-50 dark:hover:bg-emerald-900/20'
      : 'text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5'}"
  >
    {#if busy}
      <svg class="animate-spin w-3.5 h-3.5" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
      </svg>
    {:else if secured}
      <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
        />
      </svg>
    {:else}
      <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d="M8 11V7a4 4 0 118 0m-4 8v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2z"
        />
      </svg>
    {/if}
  </button>
{:else if secured}
  <span class="shrink-0 -ml-1 p-1 inline-flex items-center text-emerald-500" aria-label={m.sites_tls_on()}>
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="2"
        d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
      />
    </svg>
  </span>
{/if}
