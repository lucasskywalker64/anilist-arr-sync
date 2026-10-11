<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { getApiKey } from '../lib/api';

  export let activeTab = 'sync';
  export let isSyncing = false;
  export let stagedCount = 0;
  export let reviewCount = 0;
  export let currentUser: string | null = null;

  const dispatch = createEventDispatcher();

  function selectTab(tab: string) {
    dispatch('changeTab', tab);
  }

  function openApiKeyModal() {
    dispatch('openApiKeyModal');
  }

  function handleLogout() {
    dispatch('logout');
  }
</script>

<header class="sticky top-0 z-40 bg-[#152232]/90 backdrop-blur border-b border-[#24374e]">
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
    <div class="flex items-center justify-between h-16">
      <!-- Logo and Brand -->
      <div class="flex items-center gap-3">
        <div class="w-9 h-9 rounded-lg bg-[#0b1622] border border-[#24374e] flex items-center justify-center text-sky-400 font-bold text-lg shadow-inner">
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
          </svg>
        </div>
        <div>
          <span class="text-base font-bold tracking-tight text-white">AniList Arr Sync</span>
          <div class="flex items-center gap-1.5 text-[11px] text-slate-400">
            {#if isSyncing}
              <span class="inline-block w-2 h-2 rounded-full bg-amber-400 animate-pulse"></span>
              <span class="text-amber-400 font-medium">Syncing pass active</span>
            {:else}
              <span class="inline-block w-2 h-2 rounded-full bg-emerald-400"></span>
              <span>Sync daemon idle</span>
            {/if}
          </div>
        </div>
      </div>

      <!-- Navigation tabs -->
      <nav class="hidden md:flex items-center gap-1" aria-label="Main Navigation">
        <button
          type="button"
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition {activeTab === 'sync' ? 'bg-sky-500/15 text-sky-400 border border-sky-500/30' : 'text-slate-300 hover:text-white hover:bg-[#19273a]'}"
          on:click={() => selectTab('sync')}
        >
          Sync Dashboard
        </button>

        <button
          type="button"
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition relative {activeTab === 'staged' ? 'bg-sky-500/15 text-sky-400 border border-sky-500/30' : 'text-slate-300 hover:text-white hover:bg-[#19273a]'}"
          on:click={() => selectTab('staged')}
        >
          Staged Actions
          {#if stagedCount > 0}
            <span class="ml-1.5 px-1.5 py-0.5 text-[10px] font-bold rounded-full bg-amber-500 text-slate-950">
              {stagedCount}
            </span>
          {/if}
        </button>

        <button
          type="button"
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition relative {activeTab === 'review' ? 'bg-sky-500/15 text-sky-400 border border-sky-500/30' : 'text-slate-300 hover:text-white hover:bg-[#19273a]'}"
          on:click={() => selectTab('review')}
        >
          Review Queue
          {#if reviewCount > 0}
            <span class="ml-1.5 px-1.5 py-0.5 text-[10px] font-bold rounded-full bg-rose-500 text-white">
              {reviewCount}
            </span>
          {/if}
        </button>

        <button
          type="button"
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition {activeTab === 'overrides' ? 'bg-sky-500/15 text-sky-400 border border-sky-500/30' : 'text-slate-300 hover:text-white hover:bg-[#19273a]'}"
          on:click={() => selectTab('overrides')}
        >
          Overrides
        </button>

        <button
          type="button"
          class="px-3 py-1.5 rounded-lg text-sm font-medium transition {activeTab === 'settings' ? 'bg-sky-500/15 text-sky-400 border border-sky-500/30' : 'text-slate-300 hover:text-white hover:bg-[#19273a]'}"
          on:click={() => selectTab('settings')}
        >
          Settings
        </button>
      </nav>

      <!-- Right controls (API Key & Auth) -->
      <div class="flex items-center gap-2">
        <button
          type="button"
          on:click={openApiKeyModal}
          title="Configure API Key"
          class="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium rounded-lg border border-[#24374e] bg-[#0b1622] hover:bg-[#19273a] text-slate-300 transition"
        >
          <span class="inline-block w-2 h-2 rounded-full {getApiKey() ? 'bg-emerald-400' : 'bg-slate-500'}"></span>
          <span>API Key</span>
        </button>

        {#if currentUser}
          <div class="flex items-center gap-2 pl-2 border-l border-[#24374e]">
            <span class="text-xs text-slate-300 font-medium">{currentUser}</span>
            <button
              type="button"
              on:click={handleLogout}
              class="text-xs text-slate-400 hover:text-rose-400 transition"
              title="Logout"
            >
              Sign out
            </button>
          </div>
        {/if}
      </div>
    </div>
  </div>

  <!-- Mobile nav bar -->
  <div class="md:hidden flex items-center justify-around border-t border-[#24374e] py-2 px-1 bg-[#152232]">
    <button
      class="px-2 py-1 text-xs font-medium rounded {activeTab === 'sync' ? 'text-sky-400 font-semibold' : 'text-slate-400'}"
      on:click={() => selectTab('sync')}
    >
      Sync
    </button>
    <button
      class="px-2 py-1 text-xs font-medium rounded {activeTab === 'staged' ? 'text-sky-400 font-semibold' : 'text-slate-400'}"
      on:click={() => selectTab('staged')}
    >
      Staged ({stagedCount})
    </button>
    <button
      class="px-2 py-1 text-xs font-medium rounded {activeTab === 'review' ? 'text-sky-400 font-semibold' : 'text-slate-400'}"
      on:click={() => selectTab('review')}
    >
      Review ({reviewCount})
    </button>
    <button
      class="px-2 py-1 text-xs font-medium rounded {activeTab === 'overrides' ? 'text-sky-400 font-semibold' : 'text-slate-400'}"
      on:click={() => selectTab('overrides')}
    >
      Overrides
    </button>
    <button
      class="px-2 py-1 text-xs font-medium rounded {activeTab === 'settings' ? 'text-sky-400 font-semibold' : 'text-slate-400'}"
      on:click={() => selectTab('settings')}
    >
      Settings
    </button>
  </div>
</header>
