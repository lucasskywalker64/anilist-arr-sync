<script lang="ts">
  import { RotateCw } from 'lucide-svelte';

  export let instanceName = 'AniList-Arr-Sync';
  export let isSyncing = false;
</script>

<header class="header">
  <div class="logoContainer">
    <svg class="logo" viewBox="0 0 100 100" width="32" height="32">
      <circle cx="50" cy="50" r="48" fill="#202020" stroke="#35c5f4" stroke-width="4" />
      <circle cx="50" cy="50" r="32" fill="#2a2a2a" />
      <path d="M50 25 L75 70 L25 70 Z" fill="#35c5f4" />
      <circle cx="50" cy="55" r="9" fill="#202020" />
    </svg>
    <span class="instanceName">{instanceName}</span>
  </div>

  <div class="right">
    {#if isSyncing}
      <div class="syncIndicator" title="Sync is running">
        <RotateCw size={16} class="spinning" />
        <span class="syncText">Syncing...</span>
      </div>
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
