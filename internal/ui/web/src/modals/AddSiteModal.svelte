<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import { browseDir } from '$stores/browse';
  import { inspectDirectory, addSite, type DirectoryReport } from '$stores/addsite';
  import { loadSites } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';

  // The domain is typed, never derived. Servlo has no TLD of its own to append
  // to a directory name, and appending one is exactly what this replaces.
  let domain = $state('');
  let path = $state('');
  let phpVersion = $state('');
  let publicDir = $state('');

  let browsing = $state(false);
  let dirs = $state<Array<{ name: string; path: string }>>([]);
  let loading = $state(false);
  let report = $state<DirectoryReport | null>(null);
  let submitting = $state(false);
  let error = $state('');
  let warning = $state('');
  let addedDomain = $state('');

  const canSubmit = $derived(domain.trim() !== '' && path.trim() !== '' && !submitting);

  async function browse(dir: string) {
    loading = true;
    error = '';
    try {
      const res = await browseDir(dir);
      if (res.error) {
        error = res.error;
        return;
      }
      path = res.current;
      dirs = res.dirs;
      await inspect();
    } finally {
      loading = false;
    }
  }

  // Inspecting is a read, so it can run whenever the path changes and show what
  // is there before anything is registered.
  async function inspect() {
    if (!path.trim()) {
      report = null;
      return;
    }
    try {
      const res = await inspectDirectory(path.trim());
      report = res;
      if (!phpVersion && res.php_version) phpVersion = res.php_version;
      if (!publicDir && res.public_dir) publicDir = res.public_dir;
    } catch {
      report = null;
    }
  }

  function openAddedSite() {
    closeModal();
    if (addedDomain) goToTab('sites', addedDomain);
  }

  async function submit() {
    submitting = true;
    error = '';
    warning = '';
    try {
      const res = await addSite({
        domain: domain.trim(),
        path: path.trim(),
        php_version: phpVersion.trim(),
        public_dir: publicDir.trim()
      });
      if (!res.ok) {
        error = res.error || m.addsite_failed();
        return;
      }
      await loadSites();
      addedDomain = res.domain ?? '';
      // Registered, but serving an empty directory. Say so rather than closing
      // onto a site that returns nothing and looks broken.
      if (res.warning) {
        warning = res.warning;
        return;
      }
      openAddedSite();
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      submitting = false;
    }
  }
</script>

<Modal open title={m.addsite_title()} onclose={closeModal}>
  <div class="px-5 py-3 space-y-3">
    <label class="block">
      <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_domain()}</span>
      <input
        bind:value={domain}
        placeholder="example.com"
        autocomplete="off"
        spellcheck="false"
        class="mt-1 w-full px-2.5 py-1.5 text-sm font-mono rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
      />
      <span class="mt-1 block text-[11px] text-gray-400 dark:text-gray-500">{m.addsite_domainHint()}</span>
    </label>

    <label class="block">
      <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_path()}</span>
      <div class="mt-1 flex gap-2">
        <input
          bind:value={path}
          onblur={inspect}
          placeholder="/home/servlo/sites/example.com"
          autocomplete="off"
          spellcheck="false"
          class="flex-1 min-w-0 px-2.5 py-1.5 text-sm font-mono rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
        />
        <button
          type="button"
          onclick={() => {
            browsing = !browsing;
            if (browsing) browse(path.trim());
          }}
          class="shrink-0 px-2.5 py-1.5 text-xs rounded-md border border-gray-200 dark:border-servlo-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
        >
          {m.addsite_browse()}
        </button>
      </div>
    </label>

    {#if browsing}
      <div class="max-h-48 overflow-y-auto rounded-md border border-gray-100 dark:border-servlo-border/60 p-1">
        {#if loading}
          <div class="py-4 text-center text-xs text-gray-400">{m.common_loading()}</div>
        {:else}
          {#each dirs as d (d.path)}
            <button
              type="button"
              onclick={() => browse(d.path)}
              class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-sm rounded-sm hover:bg-gray-50 dark:hover:bg-white/5 transition-colors {d.name ===
              '..'
                ? 'text-gray-400'
                : 'text-gray-700 dark:text-gray-300'}"
            >
              <svg class="w-4 h-4 shrink-0 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/>
              </svg>
              <span class="truncate">{d.name}</span>
            </button>
          {/each}
          {#if dirs.length === 0}
            <div class="py-4 text-center text-xs text-gray-400">{m.link_noSubdirs()}</div>
          {/if}
        {/if}
      </div>
    {/if}

    {#if report}
      <div class="text-[11px] text-gray-500 dark:text-gray-400" data-addsite-report>
        {#if !report.exists}
          {m.addsite_willCreate()}
        {:else if report.framework}
          {m.addsite_detected({ framework: report.framework })}
        {:else}
          {m.addsite_noFramework()}
        {/if}
      </div>
    {/if}

    <div class="grid grid-cols-2 gap-3">
      <label class="block">
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_phpVersion()}</span>
        <input
          bind:value={phpVersion}
          placeholder={m.addsite_auto()}
          autocomplete="off"
          class="mt-1 w-full px-2.5 py-1.5 text-sm font-mono rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
        />
      </label>
      <label class="block">
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_publicDir()}</span>
        <input
          bind:value={publicDir}
          placeholder={m.addsite_auto()}
          autocomplete="off"
          spellcheck="false"
          class="mt-1 w-full px-2.5 py-1.5 text-sm font-mono rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
        />
      </label>
    </div>
  </div>

  {#if error}
    <div class="px-5 py-2">
      <p class="text-xs text-red-500" data-addsite-error>{error}</p>
    </div>
  {/if}

  {#if warning}
    <div class="px-5 py-2">
      <p class="text-xs text-amber-600 dark:text-amber-500" data-addsite-warning>{warning}</p>
    </div>
  {/if}

  {#snippet footer()}
    {#if warning}
      <DetailButton tone="primary" onclick={openAddedSite}>{m.link_continueToSite()}</DetailButton>
    {:else}
      <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
      <DetailButton tone="primary" onclick={submit} disabled={!canSubmit} loading={submitting}>
        {m.addsite_submit()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
