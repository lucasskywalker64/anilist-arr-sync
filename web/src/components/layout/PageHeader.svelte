<script lang="ts">
  import { RotateCw, LogOut } from 'lucide-svelte';
  import { createEventDispatcher } from 'svelte';
  import LifecycleActions from './LifecycleActions.svelte';

  export let instanceName = 'AniList-Arr-Sync';
  export let isSyncing = false;
  export let authenticated = false;

  const dispatch = createEventDispatcher<{ logout: void }>();
</script>

<header class="header">
  <div class="logoContainer">
    <img class="logo" src="./favicon.svg" alt="" width="32" height="32" />
    <span class="instanceName">{instanceName}</span>
  </div>

  <div class="right">
    <LifecycleActions />
    {#if isSyncing}
      <div class="syncIndicator" title="Sync is running">
        <RotateCw size={16} class="spinning" />
        <span class="syncText">Syncing...</span>
      </div>
    {/if}
    {#if authenticated}
      <button
        type="button"
        class="logoutButton"
        title="Sign Out"
        on:click={() => dispatch('logout')}
      >
        <LogOut size={16} />
        <span>Sign Out</span>
      </button>
    {/if}
  </div>
</header>

<style>
  .header {
    height: var(--headerHeight);
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    background-color: var(--pageHeaderBackgroundColor);
    color: var(--white);
    z-index: 3;
    border-bottom: 1px solid rgba(0, 0, 0, 0.2);
  }

  .logoContainer {
    flex: 0 0 var(--sidebarWidth);
    padding-left: 20px;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .logo {
    display: block;
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    object-fit: contain;
  }

  .instanceName {
    font-size: 16px;
    font-weight: 500;
    color: var(--white);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .right {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    flex-grow: 1;
    padding-right: 20px;
    gap: 15px;
  }

  .syncIndicator {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--sonarrBlue);
    font-size: var(--smallFontSize);
  }

  .logoutButton {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.2);
    color: var(--white);
    padding: 4px 10px;
    border-radius: var(--borderRadius);
    cursor: pointer;
    font-size: var(--smallFontSize);
    transition: background-color 0.15s ease, border-color 0.15s ease;
  }

  .logoutButton:hover {
    background-color: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.4);
  }

  :global(.spinning) {
    animation: spin 1.5s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }
</style>
