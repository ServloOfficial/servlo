<script lang="ts">
  import PauseGlyph from '$components/PauseGlyph.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import { apiBase } from '$lib/api';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
    size?: string;
  }
  let { site, size = 'w-4 h-4' }: Props = $props();
</script>

<!-- Paused first, whatever else the site has. Pausing swaps the vhost for the
     paused page but leaves the FPM pool running, so fpm_running still reads true
     and every other branch here would call the site up. -->
{#if site.paused}
  <PauseGlyph size={size} class="text-gray-400 dark:text-gray-500 opacity-60" />
{:else if site.custom_container}
  <svg class="{size} {site.fpm_running ? 'text-violet-500' : 'text-gray-300 dark:text-gray-600'}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>
  </svg>
{:else if site.has_favicon}
  <img src={apiBase + '/api/sites/' + site.domain + '/favicon'} class="{size} rounded-xs object-contain" loading="lazy" alt="" />
{:else}
  <StatusDot color={site.fpm_running ? 'green' : 'gray'} />
{/if}
