<script lang="ts">
  import { onMount } from 'svelte';
  import Page from './components/layout/Page.svelte';
  import SyncDashboard from './components/views/SyncDashboard.svelte';
  import StagedPreview from './components/views/StagedPreview.svelte';
  import OverridesManager from './components/views/OverridesManager.svelte';
  import ReviewQueue from './components/views/ReviewQueue.svelte';
  import HistoryView from './components/views/HistoryView.svelte';
  import SettingsView from './components/views/SettingsView.svelte';
  import SystemView from './components/views/SystemView.svelte';
  import LoginModal from './components/views/LoginModal.svelte';
  import { api, getBaseUrl } from './lib/api';
  import type { ReviewItem, StagedAction, OverrideItem, CurrentUser } from './lib/types';

  let currentRoute = '/';
  let currentUser: CurrentUser | null = null;
  let needsLogin = false;
  let isSetupMode = false;
  let isSyncing = false;
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

  function handleHashChange() {
    const hash = window.location.hash;
    if (hash) {
      currentRoute = normalizeRoute(hash);
    } else {
      const urlBase = getBaseUrl();
      let path = window.location.pathname;
      if (urlBase && path.startsWith(urlBase)) {
        path = path.slice(urlBase.length);
      }
      currentRoute = normalizeRoute(path);
    }
  }

  function navigate(path: string) {
    currentRoute = normalizeRoute(path);
    window.location.hash = currentRoute;
  }

  async function checkAuthAndBoot() {
    try {
      currentUser = await api.getUser();
      needsLogin = !currentUser.authenticated;
    } catch (e: any) {
      if (e.status === 401 || (e.message && e.message.includes('401'))) {
        needsLogin = true;
      }
    }

    if (!needsLogin) {
      await loadGlobalData();
    }
  }

  async function loadGlobalData() {
    try {
      const [rev, stg, ovr, syncSt] = await Promise.allSettled([
        api.getReviewQueue(),
        api.getStaged(),
        api.getOverrides(),
        api.getSyncStatus(),
      ]);

      if (rev.status === 'fulfilled') reviewItems = rev.value;
      if (stg.status === 'fulfilled') stagedActions = stg.value;
      if (ovr.status === 'fulfilled') overrides = ovr.value;
      if (syncSt.status === 'fulfilled') isSyncing = syncSt.value.isSyncing;

      try {
        const cfg = await api.getConfig();
        if (cfg.instanceName) instanceName = cfg.instanceName;
      } catch {
        // config not readable yet
      }
    } catch {
      // ignore global poll failure
    }
  }

  async function handleLogout() {
    try {
      await api.logout();
    } catch {
      // ignore
    }
    currentUser = null;
    needsLogin = true;
  }

  function handleLoginSuccess(e: CustomEvent<{ username: string }>) {
    needsLogin = false;
    currentUser = { username: e.detail.username, authenticated: true };
    loadGlobalData();
  }

  onMount(() => {
    handleHashChange();
    window.addEventListener('hashchange', handleHashChange);
    checkAuthAndBoot();

    const interval = setInterval(() => {
      if (!needsLogin) {
        loadGlobalData();
      }
    }, 10000);

    return () => {
      window.removeEventListener('hashchange', handleHashChange);
      clearInterval(interval);
    };
  });
</script>

{#if needsLogin}
  <LoginModal isSetup={isSetupMode} on:success={handleLoginSuccess} />
{:else}
  <Page
    {currentRoute}
    {instanceName}
    username={currentUser?.username || ''}
    isAuthenticated={!!currentUser?.authenticated}
    {isSyncing}
    reviewCount={reviewItems.length}
    stagedCount={stagedActions.length}
    on:navigate={(e) => navigate(e.detail)}
    on:logout={handleLogout}
  >
    <!-- View Switcher -->
    {#if currentRoute === '/'}
      <SyncDashboard
        bind:isSyncing
        {reviewItems}
        {stagedActions}
        {overrides}
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
      />
    {:else if currentRoute === '/activity/review'}
      <ReviewQueue
        bind:reviewItems
        on:refresh={loadGlobalData}
      />
    {:else if currentRoute === '/activity/history'}
      <HistoryView />
    {:else if currentRoute.startsWith('/settings')}
      <SettingsView
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
      <!-- Fallback to Dashboard -->
      <SyncDashboard
        bind:isSyncing
        {reviewItems}
        {stagedActions}
        {overrides}
        on:navigate={(e) => navigate(e.detail)}
        on:refresh={loadGlobalData}
      />
    {/if}
  </Page>
{/if}
