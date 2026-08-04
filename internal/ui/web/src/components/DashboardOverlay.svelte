<script lang="ts">
  import { dashboardOpen, closeDashboard } from '$stores/dashboard';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
  import Icon from './Icon.svelte';
  import { m } from '../paraglide/messages.js';

  let iframeEl = $state<HTMLIFrameElement | null>(null);

  function iframeWindow(): Window | null {
    try {
      return iframeEl?.contentWindow ?? null;
    } catch {
      return null;
    }
  }

  function reloadIframe() {
    const w = iframeWindow();
    if (w) {
      w.location.reload();
    } else if (iframeEl) {
      iframeEl.src = iframeEl.src; // cross-origin: reload to its entry URL
    }
  }
</script>

{#if $dashboardOpen}
  {@const d = $dashboardOpen}
  {@const iframeSrc = d.dashboard + (d.extraPath ?? '')}
  <div class="fixed top-0 right-0 left-0 bottom-16 md:left-14 md:bottom-0 z-30 flex flex-col bg-white dark:bg-servlo-bg">
    <div class="flex items-center justify-between px-3 py-3 border-b border-gray-200 dark:border-servlo-border shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <svg class="w-5 h-5 text-gray-500 dark:text-gray-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          {@html dashboardIconSvg(d.name, d.icon)}
        </svg>
        <span class="text-sm font-medium text-gray-900 dark:text-white truncate">{d.label || d.name}</span>
        <a
          href={iframeSrc}
          target="_blank"
          rel="noopener"
          class="font-mono text-[10px] text-sky-600 dark:text-sky-400 hover:underline truncate"
        >{d.dashboard}</a>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button
          onclick={reloadIframe}
          title={m.common_refresh()}
          aria-label={m.common_refresh()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          <Icon name="refresh" />
        </button>
        <a
          href={iframeSrc}
          target="_blank"
          rel="noopener"
          title={m.common_openInNewTab()}
          aria-label={m.common_openInNewTab()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        ><Icon name="external" /></a>
        <button
          onclick={closeDashboard}
          title={m.common_close()}
          aria-label={m.common_closeDashboard()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
      </div>
    </div>
    <iframe
      bind:this={iframeEl}
      src={iframeSrc}
      class="flex-1 w-full bg-white border-0"
      title={d.label || d.name}
    ></iframe>
  </div>
{/if}
