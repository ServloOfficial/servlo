<script lang="ts">
  import { onMount } from 'svelte';
  import CheckUpdatesButton from '$components/CheckUpdatesButton.svelte';
  import { version, loadVersion } from '$stores/version';
  import { accessMode } from '$stores/accessMode';
  import { lan, loadLANStatus, toggleLAN } from '$stores/lan';
  import { status } from '$stores/status';
  import {
    remoteControl,
    loadRemoteControl,
    disableRemoteControl,
    setRemoteFullAccess
  } from '$stores/remoteControl';
  import { openRemoteControlModal, openLANProgressModal, type LANAction } from '$stores/modals';
  import { autostartEnabled, loadAutostart, toggleAutostart } from '$stores/autostart';
  import Toggle from '$components/Toggle.svelte';
  import SettingsCard from '$components/SettingsCard.svelte';
  import TwoFactorSetting from './TwoFactorSetting.svelte';
  import AuditLog from './AuditLog.svelte';
  import LanguageSwitcher from '$components/LanguageSwitcher.svelte';
  import { apiFetch, apiBase } from '$lib/api';
  import { escapeHtml } from '$lib/html';
  import { m } from '../../paraglide/messages.js';

  // The remote dashboard always binds :7073; when LAN-exposed we surface the
  // address plus a scannable QR so a phone can jump straight in.
  const dashboardURL = $derived('https://' + $lan.lanIP + ':7073');
  const dashboardQRSrc = $derived(apiBase + '/api/dashboard-qr?v=' + encodeURIComponent($lan.lanIP));

  onMount(() => {
    loadLANStatus();
    loadRemoteControl();
    loadAutostart();
  });

  function startLAN(action: LANAction) {
    openLANProgressModal(action);
    toggleLAN(action);
  }

  function exposeDashboardForLAN() {
    if ($remoteControl.enabled) {
      startLAN('expose');
    } else {
      openRemoteControlModal(() => startLAN('expose'));
    }
  }

  let autostartBusy = $state(false);
  async function onToggleAutostart() {
    autostartBusy = true;
    try {
      await toggleAutostart(!$autostartEnabled);
    } finally {
      autostartBusy = false;
    }
  }


  // There is no remote session to widen while servlo is loopback-only, so the
  // setting stays out of the way. An already enabled setting keeps showing, so
  // that re-exposing does not silently hand host access back out.
  const fullAccessHidden = $derived(!$lan.exposed && !$remoteControl.fullAccess);

  // Dashboard credentials are equally inert while servlo is loopback-only, so the
  // card goes too. Configured credentials keep it visible so they can be
  // rotated or cleared, and disabled-DNS mode keeps it as its only route to
  // LAN exposure at all.
  const remoteCardHidden = $derived(
    !$lan.exposed && !$remoteControl.enabled && true
  );
  async function doDisableRemoteControl() {
    await disableRemoteControl();
  }
</script>

<div class="flex-1 overflow-y-auto">
  <div class="flex flex-wrap items-center justify-between gap-y-2 p-3 border-b border-gray-100 dark:border-servlo-border">
    <span class="font-semibold text-gray-900 dark:text-white text-base">{m.system_servlo()}</span>
    <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400 font-mono">v{$version.current}</span>
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
          <a href="https://github.com/realrashid/servlo" target="_blank" rel="noopener" class="font-medium text-servlo-red hover:text-servlo-redhov underline-offset-2 hover:underline">{m.system_servlo_starCta()}</a>
          {m.system_servlo_starAfter()}
        </p>
      </div>
    </SettingsCard>

    <TwoFactorSetting />

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
        {#if $accessMode.localControl}
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

    <SettingsCard>
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_lan_title()}</span>
        <span class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$lan.exposed ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400' : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}">
          <span class="w-1.5 h-1.5 rounded-full {$lan.exposed ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
          {$lan.exposed ? m.system_lan_exposed() : m.system_lan_loopback()}
        </span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
        {#if $lan.exposed}
          {@html m.system_lan_exposedDescription({
            ip: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">' + escapeHtml($lan.lanIP) + '</code>'
          })}
        {:else}
          {@html m.system_lan_loopbackDescription({
            loop4: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">127.0.0.1</code>',
            loop6: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">::1</code>'
          })}
        {/if}
      </p>

      {#if $lan.macos}
        <p class="text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2 mb-3">
          {@html m.system_lan_macosWarning({ pattern: '<code class="font-mono">*.test</code>' })}
        </p>
      {/if}

      {#if $accessMode.localControl}
        <div class="flex items-center gap-2">
          {#if !$lan.exposed}
            <button
              onclick={() => startLAN('expose')}
              disabled={$lan.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >{m.system_lan_expose()}</button>
          {:else}
            <button
              onclick={() => startLAN('unexpose')}
              disabled={$lan.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >{m.system_lan_stop()}</button>
          {/if}
        </div>
      {/if}

      {#if $lan.error}<p class="text-xs text-red-500 mt-2">{$lan.error}</p>{/if}
    </SettingsCard>

    {#if !remoteCardHidden}
    <SettingsCard>
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_remote_title()}</span>
        <span
          class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$remoteControl.enabled && $lan.exposed
            ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
            : $remoteControl.enabled && !$lan.exposed
              ? 'bg-amber-100 dark:bg-amber-500/15 text-amber-700 dark:text-amber-400'
              : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}"
        >
          <span class="w-1.5 h-1.5 rounded-full {$remoteControl.enabled && $lan.exposed
            ? 'bg-emerald-500'
            : $remoteControl.enabled && !$lan.exposed
              ? 'bg-amber-500'
              : 'bg-gray-400'}"></span>
          {$remoteControl.enabled && $lan.exposed ? m.system_remote_status_active() : $remoteControl.enabled && !$lan.exposed ? m.system_remote_status_inert() : m.system_remote_status_disabled()}
        </span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
        {#if false}
          {@html m.system_remote_descriptionNoDns({
            addr: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">' + ($lan.lanIP ? escapeHtml($lan.lanIP) : '&lt;lan-ip&gt;') + ':7073</code>',
            cmd: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">servlo lan:expose</code>'
          })}
        {:else}
          {@html m.system_remote_description({ loop4: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">127.0.0.1</code>', loop6: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">::1</code>' })}
        {/if}
      </p>

      {#if $accessMode.localControl}
      {#if $remoteControl.enabled}
        <div class="space-y-2">
          {#if $lan.exposed}
            <div class="flex items-center justify-between gap-3 p-3 rounded-lg bg-gray-50 dark:bg-white/3 border border-gray-100 dark:border-servlo-border">
              <div class="min-w-0">
                <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-1">{m.system_remote_address()}</p>
                <a href={dashboardURL} target="_blank" rel="noopener" class="text-sm text-teal-600 dark:text-teal-400 font-mono hover:underline break-all">{dashboardURL}</a>
              </div>
              <img src={dashboardQRSrc} width="112" height="112" alt={m.system_remote_qrAlt()} class="shrink-0 rounded-sm bg-white p-1" />
            </div>
          {/if}
          <p class="text-xs text-gray-600 dark:text-gray-400">
            {@html m.system_remote_usernameRow({ username: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded-sm font-mono">' + escapeHtml($remoteControl.username) + '</code>' })}
          </p>
          {#if !$lan.exposed && true}
            <p class="text-xs text-amber-600 dark:text-amber-400">
              {@html m.system_remote_inertWarning({ cmd: '<code class="font-mono">servlo lan:expose</code>', btn: '<em>' + m.system_lan_expose() + '</em>' })}
            </p>
          {/if}
          {#if !fullAccessHidden}
          <div class="pt-1">
            <div class="flex items-center justify-between gap-3">
              <span class="text-xs font-semibold text-gray-700 dark:text-gray-300">{m.system_remote_fullAccess_title()}</span>
              <Toggle
                on={$remoteControl.fullAccess}
                tone="amber"
                loading={$remoteControl.fullAccessLoading}
                title={m.system_remote_fullAccess_title()}
                onclick={() => setRemoteFullAccess(!$remoteControl.fullAccess)}
              />
            </div>
            <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">{m.system_remote_fullAccess_description()}</p>
            {#if $remoteControl.fullAccess}
              <p class="text-xs text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2 mt-2">
                {m.system_remote_fullAccess_warning()}
              </p>
            {/if}
          </div>
          {/if}
          <div class="flex flex-wrap gap-2">
            <button
              onclick={() => openRemoteControlModal()}
              disabled={$remoteControl.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >{m.system_remote_changeCredentials()}</button>
            <button
              onclick={doDisableRemoteControl}
              disabled={$remoteControl.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-red-50 hover:bg-red-100 dark:bg-red-500/10 dark:hover:bg-red-500/20 text-red-700 dark:text-red-400 disabled:opacity-50 transition-colors"
            >{m.system_remote_disable()}</button>
          </div>
        </div>
      {:else if false}
        <div>
          <button
            onclick={exposeDashboardForLAN}
            disabled={$lan.loading}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
          >{m.system_remote_enableDashboardLan()}</button>
        </div>
      {:else}
        <div>
          <button
            onclick={() => openRemoteControlModal()}
            disabled={!$lan.exposed}
            title={$lan.exposed ? '' : m.system_remote_enableDisabledHint()}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >{m.system_remote_enable()}</button>
          {#if !$lan.exposed}
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-2">{m.system_remote_exposeFirst()}</p>
          {/if}
        </div>
      {/if}
      {/if}
      {#if $remoteControl.error}<p class="text-xs text-red-500 mt-2">{$remoteControl.error}</p>{/if}
    </SettingsCard>
    {/if}
  </div>
</div>
