<script lang="ts">
  import { onMount } from 'svelte';
  import CheckUpdatesButton from '$components/CheckUpdatesButton.svelte';
  import { version, loadVersion } from '$stores/version';
  import { status } from '$stores/status';
  import { isAdmin } from '$stores/session';
  import { autostartEnabled, loadAutostart, toggleAutostart } from '$stores/autostart';
  import Toggle from '$components/Toggle.svelte';
  import SettingsCard from '$components/SettingsCard.svelte';
  import TwoFactorSetting from './TwoFactorSetting.svelte';
  import AuditLog from './AuditLog.svelte';
  import ServerStateCard from './ServerStateCard.svelte';
  import LanguageSwitcher from '$components/LanguageSwitcher.svelte';
  import { m } from '../../paraglide/messages.js';

  onMount(() => {
    loadAutostart();
  });

  let autostartBusy = $state(false);
  async function onToggleAutostart() {
    autostartBusy = true;
    try {
      await toggleAutostart(!$autostartEnabled);
    } finally {
      autostartBusy = false;
    }
  }
</script>

<div class="flex-1 overflow-y-auto">
  <div class="flex flex-wrap items-center justify-between gap-y-2 p-3 border-b border-gray-100 dark:border-servlo-border">
    <span class="font-semibold text-gray-900 dark:text-white text-base">{m.system_servlo()}</span>
    <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400 font-mono">v{$version.current}<span class="ml-1.5 text-[10px] uppercase tracking-wide text-amber-600 dark:text-amber-500">{m.common_beta()}</span></span>
  </div>

  <div class="p-3 space-y-3">
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
    <SettingsCard>
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 text-sm">
          {#if $version.checked && !$version.hasUpdate}
            <span class="inline-flex items-center gap-2 text-emerald-600 dark:text-emerald-500">
              <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/>
              </svg>
              {m.system_servlo_latest()}
            </span>
          {:else if $version.hasUpdate}
            <span class="inline-flex items-center gap-1.5 font-medium text-yellow-700 dark:text-yellow-400">
              <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>
              </svg>
              {m.system_servlo_available({ version: $version.latest })}
            </span>
          {/if}
        </div>
        <CheckUpdatesButton onclick={() => loadVersion(true)} checking={$version.checking} />
      </div>

      {#if $version.hasUpdate}
        <div class="space-y-3 mt-3">
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {@html m.system_servlo_updateHint({ cmd: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">servlo update</code>' })}
          </p>
          {#if $version.changelog}
            <div>
              <p class="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">{m.system_servlo_whatsNew()}</p>
              <pre class="text-xs text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-white/3 rounded-lg p-3 overflow-x-auto whitespace-pre-wrap font-mono leading-relaxed border border-gray-100 dark:border-servlo-border">{$version.changelog}</pre>
            </div>
          {/if}
        </div>
      {/if}

      <div class="flex items-start gap-2 text-xs text-gray-500 dark:text-gray-400 mt-3">
        <svg class="w-3.5 h-3.5 shrink-0 mt-0.5 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
          <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.286 3.957a1 1 0 00.95.69h4.162c.969 0 1.371 1.24.588 1.81l-3.367 2.446a1 1 0 00-.364 1.118l1.287 3.957c.3.922-.755 1.688-1.54 1.118l-3.366-2.446a1 1 0 00-1.176 0l-3.366 2.446c-.784.57-1.838-.196-1.54-1.118l1.287-3.957a1 1 0 00-.364-1.118L2.098 9.384c-.783-.57-.38-1.81.588-1.81h4.162a1 1 0 00.95-.69l1.286-3.957z"/>
        </svg>
        <p class="leading-relaxed">
          {m.system_servlo_starBlurb()}
          <a href="https://github.com/ServloOfficial/servlo" target="_blank" rel="noopener" class="font-medium text-servlo-red hover:text-servlo-redhov underline-offset-2 hover:underline">{m.system_servlo_starCta()}</a>
          {m.system_servlo_starAfter()}
        </p>
      </div>
    </SettingsCard>

    <TwoFactorSetting />

    <ServerStateCard />

    <AuditLog />

    <SettingsCard>
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_language_title()}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_language_description()}</p>
        <LanguageSwitcher />
      </div>
    </SettingsCard>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
    <SettingsCard>
      <div class="flex items-center justify-between gap-3 mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_autostart_title()}</span>
        {#if $isAdmin}
          <Toggle
            on={$autostartEnabled}
            loading={autostartBusy}
            onclick={onToggleAutostart}
            title={$autostartEnabled ? m.system_autostart_toggleOff() : m.system_autostart_toggleOn()}
          />
        {:else}
          <span class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$autostartEnabled ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400' : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}">
            <span class="w-1.5 h-1.5 rounded-full {$autostartEnabled ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
            {$autostartEnabled ? m.common_enabled() : m.common_disabled()}
          </span>
        {/if}
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_autostart_description()}</p>
    </SettingsCard>

    </div>

  </div>
</div>
