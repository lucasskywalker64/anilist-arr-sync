<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import {
    RefreshCw,
    Activity,
    Settings,
    Server,
  } from 'lucide-svelte';
  import Label from '../common/Label.svelte';

  export let currentRoute = '/';
  export let stagedCount = 0;
  export let reviewCount = 0;

  const dispatch = createEventDispatcher<{ navigate: string }>();

  function navigate(path: string) {
    dispatch('navigate', path);
  }

  // Determine parent section
  $: isSyncParent = currentRoute === '/' || currentRoute === '/staged' || currentRoute === '/overrides';
  $: isActivityParent = currentRoute.startsWith('/activity');
  $: isSettingsParent = currentRoute.startsWith('/settings');
  $: isSystemParent = currentRoute.startsWith('/system');
</script>

<aside class="sidebar">
  <nav class="nav">
    <!-- 1. Sync -->
    <div class="item {isSyncParent ? 'isActiveItem' : ''}">
      <a
        href="#/"
        class="link {isSyncParent ? 'isActiveParentLink' : ''}"
        on:click|preventDefault={() => navigate('/')}
      >
        <span class="iconContainer"><RefreshCw size={16} /></span>
        <span class="title">Sync</span>
        {#if stagedCount > 0}
          <span class="status">
            <Label kind="info" size="small">{stagedCount}</Label>
          </span>
        {/if}
      </a>
      {#if isSyncParent}
        <div class="children">
          <a
            href="#/"
            class="link childLink {currentRoute === '/' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/')}
          >
            <span class="noIcon">Dashboard</span>
          </a>
          <a
            href="#/staged"
            class="link childLink {currentRoute === '/staged' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/staged')}
          >
            <span class="noIcon">Staged Preview</span>
            {#if stagedCount > 0}
              <span class="status">
                <Label kind="info" size="small">{stagedCount}</Label>
              </span>
            {/if}
          </a>
          <a
            href="#/overrides"
            class="link childLink {currentRoute === '/overrides' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/overrides')}
          >
            <span class="noIcon">Overrides</span>
          </a>
        </div>
      {/if}
    </div>

    <!-- 2. Activity -->
    <div class="item {isActivityParent ? 'isActiveItem' : ''}">
      <a
        href="#/activity/review"
        class="link {isActivityParent ? 'isActiveParentLink' : ''}"
        on:click|preventDefault={() => navigate('/activity/review')}
      >
        <span class="iconContainer"><Activity size={16} /></span>
        <span class="title">Activity</span>
        {#if reviewCount > 0}
          <span class="status">
            <Label kind="warning" size="small">{reviewCount}</Label>
          </span>
        {/if}
      </a>
      {#if isActivityParent}
        <div class="children">
          <a
            href="#/activity/review"
            class="link childLink {currentRoute === '/activity/review' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/activity/review')}
          >
            <span class="noIcon">Review Queue</span>
            {#if reviewCount > 0}
              <span class="status">
                <Label kind="warning" size="small">{reviewCount}</Label>
              </span>
            {/if}
          </a>
          <a
            href="#/activity/history"
            class="link childLink {currentRoute === '/activity/history' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/activity/history')}
          >
            <span class="noIcon">History</span>
          </a>
        </div>
      {/if}
    </div>

    <!-- 3. Settings -->
    <div class="item {isSettingsParent ? 'isActiveItem' : ''}">
      <a
        href="#/settings/general"
        class="link {isSettingsParent ? 'isActiveParentLink' : ''}"
        on:click|preventDefault={() => navigate('/settings/general')}
      >
        <span class="iconContainer"><Settings size={16} /></span>
        <span class="title">Settings</span>
      </a>
      {#if isSettingsParent}
        <div class="children">
          <a
            href="#/settings/general"
            class="link childLink {currentRoute === '/settings/general' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/settings/general')}
          >
            <span class="noIcon">General</span>
          </a>
          <a
            href="#/settings/sync"
            class="link childLink {currentRoute === '/settings/sync' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/settings/sync')}
          >
            <span class="noIcon">Sync & AniList</span>
          </a>
          <a
            href="#/settings/servarr"
            class="link childLink {currentRoute === '/settings/servarr' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/settings/servarr')}
          >
            <span class="noIcon">Servarr</span>
          </a>
          <a
            href="#/settings/connect"
            class="link childLink {currentRoute === '/settings/connect' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/settings/connect')}
          >
            <span class="noIcon">Connect</span>
          </a>
        </div>
      {/if}
    </div>

    <!-- 4. System -->
    <div class="item {isSystemParent ? 'isActiveItem' : ''}">
      <a
        href="#/system/status"
        class="link {isSystemParent ? 'isActiveParentLink' : ''}"
        on:click|preventDefault={() => navigate('/system/status')}
      >
        <span class="iconContainer"><Server size={16} /></span>
        <span class="title">System</span>
      </a>
      {#if isSystemParent}
        <div class="children">
          <a
            href="#/system/status"
            class="link childLink {currentRoute === '/system/status' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/system/status')}
          >
            <span class="noIcon">Status</span>
          </a>
          <a
            href="#/system/backup"
            class="link childLink {currentRoute === '/system/backup' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/system/backup')}
          >
            <span class="noIcon">Backup</span>
          </a>
          <a
            href="#/system/updates"
            class="link childLink {currentRoute === '/system/updates' ? 'isActiveLink' : ''}"
            on:click|preventDefault={() => navigate('/system/updates')}
          >
            <span class="noIcon">Updates</span>
          </a>
        </div>
      {/if}
    </div>
  </nav>
</aside>

<style>
  .sidebar {
    flex: 0 0 var(--sidebarWidth);
    width: var(--sidebarWidth);
    background-color: var(--sidebarBackgroundColor);
    color: var(--white);
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    border-right: 1px solid rgba(0, 0, 0, 0.2);
  }

  .nav {
    display: flex;
    flex-direction: column;
    width: 100%;
  }

  .item {
    border-left: 3px solid transparent;
    color: var(--sidebarColor);
    transition: border-left 0.3s ease-in-out;
  }

  .item.isActiveItem {
    border-left: 3px solid var(--themeAlternateBlue);
  }

  .link {
    display: block;
    padding: 12px 24px;
    color: var(--sidebarColor);
    text-decoration: none;
    font-size: var(--defaultFontSize);
    transition: color 150ms ease, background-color 150ms ease;
  }

  .link:hover,
  .link:focus {
    color: var(--themeBlue);
    text-decoration: none;
  }

  .isActiveParentLink {
    background-color: var(--sidebarActiveBackgroundColor);
  }

  .childLink {
    padding: 10px 24px;
    background-color: rgba(0, 0, 0, 0.15);
  }

  .isActiveLink {
    color: var(--themeAlternateBlue);
    font-weight: 500;
  }

  .noIcon {
    margin-left: 25px;
  }

  .iconContainer {
    display: inline-block;
    margin-right: 7px;
    width: 18px;
    text-align: center;
    vertical-align: middle;
  }

  .title {
    vertical-align: middle;
  }

  .status {
    float: right;
  }

  .children {
    display: flex;
    flex-direction: column;
  }
</style>
