<script lang="ts">
  import { onMount } from 'svelte';
  import Page from './components/layout/Page.svelte';
  import SyncDashboard from './components/views/SyncDashboard.svelte';
  import LibraryView from './components/views/LibraryView.svelte';
  import StagedPreview from './components/views/StagedPreview.svelte';
  import OverridesManager from './components/views/OverridesManager.svelte';
  import ReviewQueue from './components/views/ReviewQueue.svelte';
  import HistoryView from './components/views/HistoryView.svelte';
  import BlocklistView from './components/views/BlocklistView.svelte';
  import SettingsView from './components/views/SettingsView.svelte';
  import SystemView from './components/views/SystemView.svelte';
  import LoginModal from './components/views/LoginModal.svelte';
  import OnboardingWizard from './components/views/OnboardingWizard.svelte';
  import { api, getBaseUrl, setApiKey } from './lib/api';
  import type { AppConfig, ReviewItem, StagedAction, OverrideItem, CurrentUser, LibraryStats } from './lib/types';

  let currentRoute = '/';
  let currentUser: CurrentUser | null = null;
  let needsLogin = false;
  let isSetupMode = false;
  let booting = true;
  let onboardingConfig: AppConfig | null = null;
  let onboardingActivated = false;
  let onboardingError = '';
  let isSyncing = false;
  let completedSyncRunId = 0;
  let libraryStats: LibraryStats | null = null;
  let instanceName = 'AniList-Arr-Sync';

  let reviewItems: ReviewItem[] = [];
  let stagedActions: StagedAction[] = [];
  let overrides: OverrideItem[] = [];

  function normalizeRoute(path: string): string {
    if (!path || path === '' || path === '#') return '/';
    if (path.startsWith('#')) path = path.slice(1);
    if (!path.startsWith('/')) path = '/' + path;
    return path;
  }

  function getRouteFromLocation(): string {
    if (typeof window === 'undefined') return '/';
    const urlBase = getBaseUrl();
    let path = window.location.pathname;
    if (urlBase && path.startsWith(urlBase)) {
      path = path.slice(urlBase.length);
    }
    if (window.location.hash && (path === '' || path === '/')) {
      return normalizeRoute(window.location.hash);
    }
    return normalizeRoute(path);
  }

  function handlePopState() {
    currentRoute = getRouteFromLocation();
  }

  function navigate(path: string) {
    currentRoute = normalizeRoute(path);
    if (typeof window !== 'undefined') {
      const urlBase = getBaseUrl();
      const fullPath = `${urlBase}${currentRoute}`;
      if (window.location.pathname !== fullPath) {
        window.history.pushState(null, '', fullPath);
      }
    }
  }

  async function checkAuthAndBoot() {
    booting = true;
    onboardingError = '';
    try {
      currentUser = await api.getUser();
      isSetupMode = Boolean(currentUser.setupRequired);
      needsLogin = isSetupMode || !currentUser.authenticated;
    } catch (e: any) {
      if (e?.setupRequired) {
        isSetupMode = true;
      }
      if (e?.status === 401 || (e?.message && e.message.includes('401'))) {
        needsLogin = true;
      }
    }

    onboardingActivated = Boolean(currentUser?.wizardCompleted && new URLSearchParams(window.location.search).get('onboarding') === 'complete');
    const showOnboarding = currentUser?.onboardingRequired && !sessionStorage.getItem(`${getBaseUrl()}:onboarding-dismissed`);
    if (!needsLogin && (onboardingActivated || showOnboarding)) {
      try {
        onboardingConfig = await api.getConfig();
      } catch (err) {
        onboardingError = err instanceof Error ? err.message : 'Could not load setup.';
      }
    }
    booting = false;
    if (!needsLogin && !onboardingConfig && !onboardingError) void loadGlobalData();
  }

  function closeOnboarding(finished: boolean) {
    onboardingConfig = null;
    if (finished) {
      if (onboardingActivated) {
        const url = new URL(window.location.href);
        url.searchParams.delete('onboarding');
        window.history.replaceState(null, '', url);
        onboardingActivated = false;
      }
      navigate('/');
    } else {
      sessionStorage.setItem(`${getBaseUrl()}:onboarding-dismissed`, 'true');
    }
    void loadGlobalData();
  }

  async function loadGlobalData() {
    try {
      const [rev, stg, ovr, syncSt, stats] = await Promise.allSettled([
        api.getReviewQueue(),
        api.getStaged(),
        api.getOverrides(),
        api.getSyncStatus(),
        api.getLibraryStats(),
      ]);

      const settled = [rev, stg, ovr, syncSt, stats];
      for (const res of settled) {
        if (res.status === 'rejected') {
          const reason = res.reason;
          if (reason && (reason.status === 401 || (reason.message && reason.message.includes('401')))) {
            needsLogin = true;
            return;
          }
        }
      }

      if (rev.status === 'fulfilled') reviewItems = rev.value;
      if (stg.status === 'fulfilled') stagedActions = stg.value;
      if (ovr.status === 'fulfilled') overrides = ovr.value;
      if (syncSt.status === 'fulfilled') {
        isSyncing = syncSt.value.isSyncing;
        if (!isSyncing) completedSyncRunId = syncSt.value.lastSync?.runId ?? 0;
      }
      libraryStats = stats.status === 'fulfilled' ? stats.value : null;

      try {
        const cfg = await api.getConfig();
        if (cfg.instanceName) instanceName = cfg.instanceName;
        if (cfg.authenticationMethod && currentUser) {
          currentUser = { ...currentUser, authMethod: cfg.authenticationMethod };
        }
      } catch (err: any) {
        if (err && (err.status === 401 || (err.message && err.message.includes('401')))) {
          needsLogin = true;
          return;
        }
      }
    } catch {
      // ignore global poll failure
    }
  }

  async function handleLogout() {
    try {
      await api.logout();
      setApiKey('');
    } catch {
      return;
    }
    currentUser = null;
    needsLogin = true;
  }

  onMount(() => {
    currentRoute = getRouteFromLocation();
    window.addEventListener('popstate', handlePopState);
    window.addEventListener('hashchange', handlePopState);
    checkAuthAndBoot();

    const interval = setInterval(() => {
      if (!booting && !needsLogin && !onboardingConfig && !onboardingError) {
        loadGlobalData();
      }
    }, 10000);

    return () => {
      window.removeEventListener('popstate', handlePopState);
      window.removeEventListener('hashchange', handlePopState);
      clearInterval(interval);
    };
  });
</script>

{#if booting}
  <p role="status">Loading application...</p>
{:else if needsLogin}
  <LoginModal isSetup={isSetupMode} on:success={checkAuthAndBoot} />
{:else if onboardingError}
  <p role="alert">{onboardingError}</p>
  <button on:click={checkAuthAndBoot}>Retry Setup</button>
{:else if onboardingConfig}
  <OnboardingWizard config={onboardingConfig} activated={onboardingActivated} on:dismiss={() => closeOnboarding(false)} on:finish={() => closeOnboarding(true)} />
{:else}
  <Page
    {currentRoute}
    {instanceName}
    {isSyncing}
    reviewCount={reviewItems.length}
    stagedCount={stagedActions.length}
    authenticated={Boolean(currentUser?.authenticated && (currentUser?.authMethod ?? 'Forms') === 'Forms')}
    on:navigate={(e) => navigate(e.detail)}
    on:logout={handleLogout}
  >
    <!-- View Switcher -->
    {#if currentRoute === '/library'}
      <LibraryView
        stats={libraryStats}
        {completedSyncRunId}
        on:navigate={(e) => navigate(e.detail)}
        on:refresh={loadGlobalData}
      />
    {:else if currentRoute === '/staged'}
      <StagedPreview
        bind:stagedActions
        on:refresh={loadGlobalData}
      />
    {:else if currentRoute === '/overrides'}
      <OverridesManager
        bind:overrides
        on:refresh={loadGlobalData}
        on:navigate={(e) => navigate(e.detail)}
      />
    {:else if currentRoute === '/activity/review'}
      <ReviewQueue
        bind:reviewItems
        on:refresh={loadGlobalData}
      />
    {:else if currentRoute === '/activity/history'}
      <HistoryView />
    {:else if currentRoute === '/activity/blocklist' || currentRoute === '/activity/ignored'}
      <BlocklistView />
    {:else if currentRoute.startsWith('/settings')}
      <SettingsView
        openOnboarding={(config) => onboardingConfig = config}
        subTab={currentRoute === '/settings/sync'
          ? 'sync'
          : currentRoute === '/settings/servarr'
          ? 'servarr'
          : currentRoute === '/settings/connect'
          ? 'connect'
          : 'general'}
      />
    {:else if currentRoute.startsWith('/system')}
      <SystemView
        subTab={currentRoute === '/system/backup'
          ? 'backup'
          : currentRoute === '/system/updates'
          ? 'updates'
          : 'status'}
      />
    {:else}
      <!-- Dashboard (handles '/' and route fallback) -->
      <SyncDashboard
        bind:isSyncing
        {libraryStats}
        {reviewItems}
        {stagedActions}
        {overrides}
        on:navigate={(e) => navigate(e.detail)}
        on:refresh={loadGlobalData}
      />
    {/if}
  </Page>
{/if}
