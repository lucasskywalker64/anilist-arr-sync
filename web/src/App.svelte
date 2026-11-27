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
  import Modal from './components/common/Modal.svelte';
  import Button from './components/common/Button.svelte';
  import { api, getBaseUrl, setApiKey } from './lib/api';
  import type { AppConfig, ReviewItem, StagedAction, OverrideItem, CurrentUser, LibraryStats } from './lib/types';

  let currentRoute = '/';
  let currentUser: CurrentUser | null = null;
  let needsLogin = false;
  let isSetupMode = false;
  let booting = true;
  let onboardingConfig: AppConfig | null = null;
  let resumeOnboarding = false;
  let onboardingError = '';
  let isSyncing = false;
  let completedSyncRunId = 0;
  let libraryStats: LibraryStats | null = null;
  let instanceName = 'AniList-Arr-Sync';

  let reviewItems: ReviewItem[] = [];
  let stagedActions: StagedAction[] = [];
  let overrides: OverrideItem[] = [];

  type RouteHistoryState = { navIndex?: number };

  let settingsView: SettingsView | null = null;
  let pendingSettingsView: SettingsView | null = null;
  let pendingNavigationRoute: string | null = null;
  let pendingHistoryTargetIndex: number | null = null;
  let savingBeforeLeave = false;
  let currentNavIndex = 0;
  let lastBrowserNavIndex = 0;
  let restoringToNavIndex: number | null = null;
  let inFlightRestoreStep = 0;
  let leavingToNavIndex: number | null = null;

  function readNavIndex(state: unknown): number | null {
    if (state && typeof state === 'object' && typeof (state as RouteHistoryState).navIndex === 'number') {
      return (state as RouteHistoryState).navIndex!;
    }
    return null;
  }

  function clearPendingNavigation() {
    pendingNavigationRoute = null;
    pendingHistoryTargetIndex = null;
    pendingSettingsView = null;
  }

  $: if (pendingSettingsView && settingsView !== pendingSettingsView) {
    clearPendingNavigation();
    savingBeforeLeave = false;
  }

  function isSettingsRoute(path: string): boolean {
    return path === '/settings' || path.startsWith('/settings/');
  }

  function shouldGuardSettingsNavigation(nextRoute: string): boolean {
    return isSettingsRoute(currentRoute) && !isSettingsRoute(nextRoute) && Boolean(settingsView?.hasUnsavedChanges());
  }

  function syncLocationToCurrentRoute() {
    if (typeof window === 'undefined') return;
    const urlBase = getBaseUrl();
    const fullPath = `${urlBase}${currentRoute}`;
    if (window.location.pathname !== fullPath || window.location.hash) {
      currentNavIndex += 1;
      lastBrowserNavIndex = currentNavIndex;
      window.history.pushState({ navIndex: currentNavIndex }, '', fullPath);
    }
  }

  function commitNavigation(path: string) {
    currentRoute = normalizeRoute(path);
    clearPendingNavigation();
    restoringToNavIndex = null;
    inFlightRestoreStep = 0;
    leavingToNavIndex = null;
    syncLocationToCurrentRoute();
  }

  function completePendingNavigation() {
    if (!pendingNavigationRoute) return;
    const target = pendingNavigationRoute;
    const targetIndex = pendingHistoryTargetIndex;
    const hadInFlightRestore = inFlightRestoreStep !== 0;
    clearPendingNavigation();
    if (targetIndex !== null && typeof window !== 'undefined') {
      const browserIndex = readNavIndex(window.history.state) ?? lastBrowserNavIndex;
      currentRoute = target;
      currentNavIndex = targetIndex;
      restoringToNavIndex = null;
      inFlightRestoreStep = 0;
      if (hadInFlightRestore) {
        leavingToNavIndex = targetIndex;
      } else if (targetIndex !== browserIndex) {
        leavingToNavIndex = targetIndex;
        window.history.go(targetIndex - browserIndex);
      } else {
        leavingToNavIndex = null;
        lastBrowserNavIndex = targetIndex;
      }
      return;
    }
    commitNavigation(target);
  }

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

  function handlePopState(event?: PopStateEvent | HashChangeEvent) {
    const isPopState = event?.type === 'popstate';
    const targetNavIndex = isPopState ? readNavIndex(window.history.state) : null;

    if (isPopState && leavingToNavIndex !== null) {
      if (targetNavIndex !== null) {
        lastBrowserNavIndex = targetNavIndex;
        if (targetNavIndex === leavingToNavIndex) {
          leavingToNavIndex = null;
          currentNavIndex = targetNavIndex;
          currentRoute = getRouteFromLocation();
        } else {
          window.history.go(leavingToNavIndex - targetNavIndex);
        }
      }
      return;
    }

    if (isPopState && restoringToNavIndex !== null) {
      if (targetNavIndex === null) {
        restoringToNavIndex = null;
        inFlightRestoreStep = 0;
        syncLocationToCurrentRoute();
        return;
      }
      const actualStep = targetNavIndex - lastBrowserNavIndex;
      lastBrowserNavIndex = targetNavIndex;
      if (targetNavIndex === restoringToNavIndex) {
        restoringToNavIndex = null;
        inFlightRestoreStep = 0;
        return;
      }
      if (inFlightRestoreStep !== 0 && Math.sign(actualStep) === Math.sign(inFlightRestoreStep)) {
        const remainingStep = restoringToNavIndex - targetNavIndex;
        inFlightRestoreStep = remainingStep;
        window.history.go(remainingStep);
        return;
      }
      const nextRoute = getRouteFromLocation();
      if (!savingBeforeLeave && shouldGuardSettingsNavigation(nextRoute)) {
        pendingNavigationRoute = nextRoute;
        pendingSettingsView = settingsView;
        pendingHistoryTargetIndex = targetNavIndex;
      }
      if (inFlightRestoreStep === 0) {
        const remainingStep = restoringToNavIndex - targetNavIndex;
        inFlightRestoreStep = remainingStep;
        window.history.go(remainingStep);
      }
      return;
    }

    const nextRoute = getRouteFromLocation();
    const delta = targetNavIndex !== null ? targetNavIndex - currentNavIndex : null;
    if (targetNavIndex !== null) {
      lastBrowserNavIndex = targetNavIndex;
    }

    if (delta === 0 && nextRoute === currentRoute) {
      return;
    }

    if (savingBeforeLeave) {
      if (delta !== null && delta !== 0) {
        restoringToNavIndex = currentNavIndex;
        inFlightRestoreStep = -delta;
        window.history.go(-delta);
      } else {
        syncLocationToCurrentRoute();
      }
      return;
    }

    if (shouldGuardSettingsNavigation(nextRoute)) {
      pendingNavigationRoute = nextRoute;
      pendingSettingsView = settingsView;
      if (delta !== null && delta !== 0 && targetNavIndex !== null) {
        pendingHistoryTargetIndex = targetNavIndex;
        restoringToNavIndex = currentNavIndex;
        inFlightRestoreStep = -delta;
        window.history.go(-delta);
      } else {
        pendingHistoryTargetIndex = null;
        syncLocationToCurrentRoute();
      }
      return;
    }

    if (targetNavIndex !== null) {
      currentNavIndex = targetNavIndex;
    }
    currentRoute = nextRoute;
    clearPendingNavigation();
  }

  function navigate(path: string) {
    const nextRoute = normalizeRoute(path);
    if (savingBeforeLeave) return;
    if (shouldGuardSettingsNavigation(nextRoute)) {
      pendingNavigationRoute = nextRoute;
      pendingSettingsView = settingsView;
      pendingHistoryTargetIndex = null;
      return;
    }
    commitNavigation(nextRoute);
  }

  function handleKeepEditing() {
    if (savingBeforeLeave) return;
    clearPendingNavigation();
  }

  function handleLeaveWithoutSaving() {
    if (savingBeforeLeave || !pendingNavigationRoute) return;
    completePendingNavigation();
  }

  async function handleSaveAndLeave() {
    const originView = settingsView;
    if (savingBeforeLeave || !pendingNavigationRoute || !originView) return;
    if (!originView.hasUnsavedChanges()) {
      completePendingNavigation();
      return;
    }
    savingBeforeLeave = true;
    let saved = false;
    try {
      saved = await originView.saveConfig();
    } finally {
      if (settingsView === originView) {
        savingBeforeLeave = false;
      }
    }
    if (settingsView !== originView) return;
    if (saved && !originView.hasUnsavedChanges() && pendingNavigationRoute) {
      completePendingNavigation();
    } else {
      clearPendingNavigation();
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

    resumeOnboarding = Boolean(currentUser?.wizardCompleted && new URLSearchParams(window.location.search).get('onboarding') === 'complete');
    const showOnboarding = currentUser?.onboardingRequired && !sessionStorage.getItem(`${getBaseUrl()}:onboarding-dismissed`);
    if (!needsLogin && (resumeOnboarding || showOnboarding)) {
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
      const url = new URL(window.location.href);
      if (url.searchParams.get('onboarding') === 'complete') {
        url.searchParams.delete('onboarding');
        window.history.replaceState(null, '', url);
      }
      resumeOnboarding = false;
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
    const existingNavIndex = readNavIndex(window.history.state);
    if (existingNavIndex !== null) {
      currentNavIndex = existingNavIndex;
      lastBrowserNavIndex = existingNavIndex;
    } else {
      currentNavIndex = 0;
      lastBrowserNavIndex = 0;
      const baseState = window.history.state && typeof window.history.state === 'object' ? window.history.state : {};
      window.history.replaceState({ ...baseState, navIndex: currentNavIndex }, '', window.location.href);
    }
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
  <OnboardingWizard config={onboardingConfig} resume={resumeOnboarding} on:dismiss={() => closeOnboarding(false)} on:finish={() => closeOnboarding(true)} />
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
    {:else if isSettingsRoute(currentRoute)}
      <SettingsView
        bind:this={settingsView}
        openOnboarding={currentUser?.onboardingRequired ? (config) => onboardingConfig = config : undefined}
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

  <Modal
    isOpen={pendingNavigationRoute !== null}
    title="Unsaved Changes"
    size="medium"
    on:close={handleKeepEditing}
  >
    <p>You have unsaved changes in Settings. If you leave now, your unsaved changes will be lost.</p>
    <svelte:fragment slot="footer">
      <Button kind="default" disabled={savingBeforeLeave} on:click={handleKeepEditing}>
        Keep editing
      </Button>
      <Button kind="danger" disabled={savingBeforeLeave} on:click={handleLeaveWithoutSaving}>
        Leave without saving
      </Button>
      <Button kind="primary" disabled={savingBeforeLeave} on:click={handleSaveAndLeave}>
        Save and leave
      </Button>
    </svelte:fragment>
  </Modal>
{/if}
