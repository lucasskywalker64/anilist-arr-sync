<script lang="ts">
  import { onMount } from 'svelte';
  import Navbar from './components/Navbar.svelte';
  import SyncDashboard from './components/SyncDashboard.svelte';
  import StagedPreview from './components/StagedPreview.svelte';
  import ReviewQueue from './components/ReviewQueue.svelte';
  import OverridesManager from './components/OverridesManager.svelte';
  import Settings from './components/Settings.svelte';
  import ApiKeyModal from './components/ApiKeyModal.svelte';
  import { api, type UserInfo } from './lib/api';

  let activeTab = 'sync';
  let isSyncing = false;
  let stagedCount = 0;
  let reviewCount = 0;
  let apiKeyModalOpen = false;

  let currentUser: string | null = null;
  let loginRequired = false;
  let loginUsername = '';
  let loginPassword = '';
  let loginError = '';
  let loggingIn = false;

  // Read initial hash or path
  function getTabFromLocation(): string {
    const hash = window.location.hash.replace('#', '').trim();
    if (['sync', 'staged', 'review', 'overrides', 'settings'].includes(hash)) {
      return hash;
    }
    const path = window.location.pathname.replace(/^\/+/, '').split('/')[0];
    if (['sync', 'staged', 'review', 'overrides', 'settings'].includes(path)) {
      return path;
    }
    return 'sync';
  }

  function handleTabChange(event: CustomEvent<string>) {
    activeTab = event.detail;
    window.location.hash = activeTab;
  }

  async function checkAuthAndCounts() {
    try {
      const user = await api.getCurrentUser();
      currentUser = user.username;
      loginRequired = false;
    } catch (err: any) {
      if (err.status === 401) {
        loginRequired = true;
      }
    }

    try {
      const [staged, review] = await Promise.all([
        api.getStagedActions(),
        api.getReviewQueue(),
      ]);
      stagedCount = staged.filter((i) => i.status === 'PENDING').length;
      reviewCount = review.length;
    } catch {
      // Continue silently if unauthenticated
    }
  }

  async function handleLogin() {
    loggingIn = true;
    loginError = '';
    try {
      const res = await api.login(loginUsername, loginPassword);
      currentUser = res.user;
      loginRequired = false;
      await checkAuthAndCounts();
    } catch (err: any) {
      loginError = err.message || 'Invalid username or password';
    } finally {
      loggingIn = false;
    }
  }

  async function handleLogout() {
    try {
      await api.logout();
      currentUser = null;
      await checkAuthAndCounts();
    } catch (err: any) {
      console.error('Logout error:', err);
    }
  }

  onMount(() => {
    activeTab = getTabFromLocation();
    window.addEventListener('hashchange', () => {
      activeTab = getTabFromLocation();
    });
    checkAuthAndCounts();
  });
</script>

<div class="min-h-screen bg-[#0b1622] text-slate-100 flex flex-col font-sans">
  <Navbar
    {activeTab}
    {isSyncing}
    {stagedCount}
    {reviewCount}
    {currentUser}
    on:changeTab={handleTabChange}
    on:openApiKeyModal={() => (apiKeyModalOpen = true)}
    on:logout={handleLogout}
  />

  <main class="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-8">
    {#if loginRequired}
      <div class="max-w-md mx-auto my-12 p-6 rounded-2xl bg-[#152232] border border-[#24374e] shadow-2xl space-y-4">
        <div class="text-center space-y-1">
          <div class="w-12 h-12 mx-auto rounded-xl bg-[#0b1622] border border-[#24374e] flex items-center justify-center text-sky-400">
            <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
          </div>
          <h2 class="text-lg font-bold text-white tracking-tight">Sign In</h2>
          <p class="text-xs text-slate-400">Forms authentication is enabled on this instance</p>
        </div>

        {#if loginError}
          <div class="p-2.5 rounded-lg bg-rose-950/60 border border-rose-800 text-rose-300 text-xs">
            {loginError}
          </div>
        {/if}

        <form on:submit|preventDefault={handleLogin} class="space-y-3.5">
          <div class="space-y-1">
            <label for="login-username" class="text-xs font-medium text-slate-300">Username</label>
            <input
              id="login-username"
              type="text"
              bind:value={loginUsername}
              placeholder="Username"
              required
              class="w-full px-3 py-2 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
            />
          </div>

          <div class="space-y-1">
            <label for="login-password" class="text-xs font-medium text-slate-300">Password</label>
            <input
              id="login-password"
              type="password"
              bind:value={loginPassword}
              placeholder="Password"
              required
              class="w-full px-3 py-2 text-xs rounded-lg bg-[#0b1622] border border-[#24374e] text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-500"
            />
          </div>

          <button
            type="submit"
            disabled={loggingIn}
            class="w-full py-2 text-xs font-semibold rounded-lg bg-sky-500 hover:bg-sky-400 text-slate-950 transition disabled:opacity-50 mt-2 shadow-lg shadow-sky-500/20"
          >
            {loggingIn ? 'Signing in...' : 'Sign In'}
          </button>
        </form>
      </div>
    {:else}
      {#if activeTab === 'sync'}
        <SyncDashboard bind:isSyncing />
      {:else if activeTab === 'staged'}
        <StagedPreview
          bind:stagedCount
          on:countChange={(e) => (stagedCount = e.detail)}
        />
      {:else if activeTab === 'review'}
        <ReviewQueue
          bind:reviewCount
          on:countChange={(e) => (reviewCount = e.detail)}
        />
      {:else if activeTab === 'overrides'}
        <OverridesManager />
      {:else if activeTab === 'settings'}
        <Settings />
      {/if}
    {/if}
  </main>

  <footer class="border-t border-[#24374e]/50 py-4 text-center text-xs text-slate-500">
    AniList Arr Sync &bull; Automated anime sync and review engine
  </footer>

  <ApiKeyModal bind:open={apiKeyModalOpen} on:saved={checkAuthAndCounts} />
</div>
