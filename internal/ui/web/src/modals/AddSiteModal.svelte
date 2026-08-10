<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import { browseDir } from '$stores/browse';
  import {
    inspectDirectory,
    addSite,
    cloneSite,
    uploadSite,
    deployKeyFor,
    testClone,
    loadApps,
    installApp,
    type DirectoryReport,
    type CloneTestResult,
    type AppOption,
    type AppInstallResult
  } from '$stores/addsite';
  import { loadSites } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';

  // The domain is typed, never derived. Servlo has no TLD of its own to append
  // to a directory name, and appending one is exactly what this replaces.
  let domain = $state('');
  let path = $state('');
  let phpVersion = $state('');
  let publicDir = $state('');

  // Four sources, one form. The fields they share are the same fields, so each
  // source is a few extra controls rather than a second modal that drifts.
  let source = $state<'folder' | 'clone' | 'upload' | 'app'>('folder');
  let archive = $state<File | null>(null);
  let repository = $state('');
  let deployKey = $state('');
  let keyLoading = $state(false);
  let copied = $state(false);
  let testing = $state(false);
  let testResult = $state<CloneTestResult | null>(null);

  // The app source. apps is loaded once, the first time the tab is opened: the
  // store is embedded in the binary, so there is nothing to refresh.
  let apps = $state<AppOption[]>([]);
  let appsLoaded = $state(false);
  let appName = $state('');
  let adminEmail = $state('');
  let installed = $state<AppInstallResult | null>(null);
  const chosenApp = $derived(apps.find((a) => a.name === appName) ?? null);

  let browsing = $state(false);
  let dirs = $state<Array<{ name: string; path: string }>>([]);
  let loading = $state(false);
  let report = $state<DirectoryReport | null>(null);
  let submitting = $state(false);
  let error = $state('');
  let warning = $state('');
  let addedDomain = $state('');

  const canSubmit = $derived(
    domain.trim() !== '' &&
      path.trim() !== '' &&
      (source !== 'clone' || repository.trim() !== '') &&
      (source !== 'upload' || archive !== null) &&
      (source !== 'app' || appName !== '') &&
      !submitting
  );

  // Switching source clears what belonged to the one being left. Otherwise a
  // clone that failed leaves its reason on screen under the folder form, where
  // it describes something the operator is no longer doing.
  function pickSource(next: 'folder' | 'clone' | 'upload' | 'app') {
    if (next === source) return;
    source = next;
    error = '';
    warning = '';
    testResult = null;
    if (next === 'app' && !appsLoaded) loadAppOptions();
  }

  async function loadAppOptions() {
    try {
      apps = await loadApps();
      appsLoaded = true;
      if (!appName && apps.length > 0) appName = apps[0].name;
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    }
  }

  // The key is per site, so it cannot be minted until the domain is typed.
  async function loadDeployKey() {
    if (!domain.trim()) {
      error = m.addsite_domainFirst();
      return;
    }
    keyLoading = true;
    error = '';
    try {
      const res = await deployKeyFor(domain.trim());
      if (res.error) {
        error = res.error;
        return;
      }
      deployKey = res.public ?? '';
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      keyLoading = false;
    }
  }

  // Copying the one-time password. Shown once and held nowhere else, so asking
  // the operator to select it by hand is asking them to lose it.
  let copiedPassword = $state(false);
  async function copyPassword() {
    try {
      await navigator.clipboard.writeText(installed?.admin_password ?? '');
      copiedPassword = true;
      setTimeout(() => (copiedPassword = false), 1500);
    } catch {
      // Clipboard denied. The password is on screen and selectable either way.
    }
  }

  async function copyKey() {
    try {
      await navigator.clipboard.writeText(deployKey);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      // Clipboard denied. The key is on screen and selectable either way.
    }
  }

  async function runTest() {
    testing = true;
    testResult = null;
    error = '';
    try {
      testResult = await testClone({ domain: domain.trim(), repository: repository.trim() });
    } catch (e) {
      testResult = { ok: false, reason: e instanceof Error ? e.message : m.common_failed() };
    } finally {
      testing = false;
    }
  }

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
      if (source === 'app') {
        const app = await installApp({
          app: appName,
          domain: domain.trim(),
          path: path.trim(),
          admin_email: adminEmail.trim()
        });
        if (!app.ok) {
          error = app.error || m.addsite_failed();
          return;
        }
        await loadSites();
        addedDomain = app.domain ?? '';
        // Stops here rather than closing. The password is generated, shown
        // once, and held nowhere else, so closing onto the site would lose it.
        installed = app;
        return;
      }
      const res =
        source === 'upload'
        ? await uploadSite({
            domain: domain.trim(),
            path: path.trim(),
            archive: archive as File,
            php_version: phpVersion.trim(),
            public_dir: publicDir.trim()
          })
        : source === 'clone'
          ? await cloneSite({
              domain: domain.trim(),
              path: path.trim(),
              repository: repository.trim(),
              php_version: phpVersion.trim(),
              public_dir: publicDir.trim()
            })
          : await addSite({
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
  {#if installed}
    <!-- Shown once. The password is generated at install and held nowhere
         else, so there is no screen that can show it again. -->
    <div class="px-5 py-3 space-y-3" data-app-installed>
      <p class="text-sm text-gray-700 dark:text-gray-200">
        {m.addsite_appInstalled({ domain: installed.domain ?? '' })}
      </p>

      {#if installed.admin_password}
        <div class="rounded-md border border-amber-300/60 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 px-3 py-2 space-y-1">
          <p class="text-[11px] font-medium text-amber-700 dark:text-amber-400">{m.addsite_appPasswordOnce()}</p>
          <dl class="text-xs text-gray-700 dark:text-gray-200 space-y-0.5">
            <div class="flex gap-2">
              <dt class="w-20 shrink-0 text-gray-500 dark:text-gray-400">{m.addsite_appAdminUser()}</dt>
              <dd class="font-mono break-all">{installed.admin_user}</dd>
            </div>
            <div class="flex gap-2 items-center">
              <dt class="w-20 shrink-0 text-gray-500 dark:text-gray-400">{m.addsite_appPassword()}</dt>
              <dd class="font-mono break-all">{installed.admin_password}</dd>
              <button
                type="button"
                onclick={copyPassword}
                class="ml-auto shrink-0 px-2 py-0.5 text-[11px] rounded-md border border-amber-300/60 dark:border-amber-500/30 text-amber-700 dark:text-amber-400 hover:bg-amber-100/60 dark:hover:bg-amber-500/10 transition-colors"
              >
                {copiedPassword ? m.common_copied() : m.common_copy()}
              </button>
            </div>
          </dl>
        </div>
      {/if}

      {#if installed.note}
        <p class="text-xs text-amber-600 dark:text-amber-500">{installed.note}</p>
      {/if}

      {#if installed.database}
        <p class="text-[11px] text-gray-400 dark:text-gray-500">
          {m.addsite_appDatabase({ name: installed.database })}
        </p>
      {/if}
    </div>
  {:else}
  <div class="px-5 py-3 space-y-3">
    <div class="flex gap-1 p-0.5 rounded-md bg-gray-100 dark:bg-white/5" role="tablist">
      {#each [{ id: 'folder' as const, label: m.addsite_sourceFolder() }, { id: 'clone' as const, label: m.addsite_sourceClone() }, { id: 'upload' as const, label: m.addsite_sourceUpload() }, { id: 'app' as const, label: m.addsite_sourceApp() }] as opt (opt.id)}
        <button
          type="button"
          role="tab"
          aria-selected={source === opt.id}
          onclick={() => pickSource(opt.id)}
          class="flex-1 px-2.5 py-1 text-xs font-medium rounded-sm transition-colors {source === opt.id
            ? 'bg-white dark:bg-white/10 text-gray-900 dark:text-gray-100 shadow-xs'
            : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'}"
        >
          {opt.label}
        </button>
      {/each}
    </div>

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

    {#if source === 'clone'}
      <label class="block">
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_repository()}</span>
        <input
          bind:value={repository}
          placeholder="git@github.com:owner/repo.git"
          autocomplete="off"
          spellcheck="false"
          class="mt-1 w-full px-2.5 py-1.5 text-sm font-mono rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
        />
      </label>

      <div class="rounded-md border border-gray-100 dark:border-servlo-border/60 p-3 space-y-2">
        <p class="text-[11px] text-gray-500 dark:text-gray-400">{m.addsite_deployKeyHint()}</p>
        {#if deployKey}
          <div class="flex items-start gap-2">
            <code
              class="flex-1 min-w-0 block px-2 py-1.5 rounded-sm bg-gray-50 dark:bg-black/30 text-[11px] font-mono text-gray-700 dark:text-gray-300 break-all"
              data-deploy-key>{deployKey}</code>
            <button
              type="button"
              onclick={copyKey}
              class="shrink-0 px-2 py-1 text-[11px] rounded-sm border border-gray-200 dark:border-servlo-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
            >
              {copied ? m.common_copied() : m.common_copy()}
            </button>
          </div>
        {:else}
          <button
            type="button"
            onclick={loadDeployKey}
            disabled={keyLoading}
            class="px-2.5 py-1 text-xs rounded-md border border-gray-200 dark:border-servlo-border text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
          >
            {keyLoading ? m.common_loading() : m.addsite_showDeployKey()}
          </button>
        {/if}

        <div class="flex items-center gap-2 pt-1">
          <button
            type="button"
            onclick={runTest}
            disabled={testing || !domain.trim() || !repository.trim()}
            class="px-2.5 py-1 text-xs rounded-md border border-gray-200 dark:border-servlo-border text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
          >
            {testing ? m.addsite_testing() : m.addsite_testConnection()}
          </button>
          {#if testResult}
            {#if testResult.ok}
              <span class="text-[11px] text-emerald-600 dark:text-emerald-500" data-clone-test-ok>
                {testResult.greeting || m.addsite_testOK()}
              </span>
            {:else}
              <span class="text-[11px] text-red-500" data-clone-test-error>{testResult.reason}</span>
            {/if}
          {/if}
        </div>
      </div>
    {/if}

    {#if source === 'app'}
      <label class="block">
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_app()}</span>
        <select
          bind:value={appName}
          class="mt-1 w-full px-2.5 py-1.5 text-sm rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
        >
          {#each apps as app (app.name)}
            <option value={app.name}>{app.label} {app.version}</option>
          {/each}
        </select>
        {#if chosenApp}
          <span class="mt-1 block text-[11px] text-gray-400 dark:text-gray-500">{chosenApp.description}</span>
        {/if}
      </label>

      {#if chosenApp}
        <ul class="text-[11px] text-gray-500 dark:text-gray-400 space-y-0.5">
          <li>{chosenApp.needs_database ? m.addsite_appWithDatabase() : m.addsite_appNoDatabase()}</li>
          <li>{chosenApp.self_setup ? m.addsite_appSelfSetup() : m.addsite_appAdminCreated()}</li>
        </ul>
      {/if}

      {#if chosenApp && !chosenApp.self_setup}
        <label class="block">
          <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_adminEmail()}</span>
          <input
            bind:value={adminEmail}
            placeholder="you@example.com"
            autocomplete="off"
            spellcheck="false"
            class="mt-1 w-full px-2.5 py-1.5 text-sm rounded-md border border-gray-200 dark:border-servlo-border bg-white dark:bg-white/[0.03] text-gray-800 dark:text-gray-100 focus:outline-hidden focus:border-servlo-red/50"
          />
        </label>
      {/if}
    {/if}

    {#if source === 'upload'}
      <label class="block">
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.addsite_archive()}</span>
        <input
          type="file"
          accept=".zip,application/zip"
          onchange={(e) => (archive = (e.currentTarget as HTMLInputElement).files?.[0] ?? null)}
          class="mt-1 w-full text-xs text-gray-600 dark:text-gray-300 file:mr-3 file:px-2.5 file:py-1 file:text-xs file:rounded-md file:border file:border-gray-200 dark:file:border-servlo-border file:bg-white dark:file:bg-white/5 file:text-gray-700 dark:file:text-gray-200"
        />
        <span class="mt-1 block text-[11px] text-gray-400 dark:text-gray-500">{m.addsite_archiveHint()}</span>
      </label>
    {/if}

    {#if source !== 'app'}
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
    {/if}
  </div>
  {/if}

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
    {#if installed}
      <DetailButton tone="primary" onclick={openAddedSite}>{m.link_continueToSite()}</DetailButton>
    {:else if warning}
      <DetailButton tone="primary" onclick={openAddedSite}>{m.link_continueToSite()}</DetailButton>
    {:else}
      <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
      <DetailButton tone="primary" onclick={submit} disabled={!canSubmit} loading={submitting}>
        {m.addsite_submit()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
