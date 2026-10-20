<script lang="ts">
  import PageHeader from './PageHeader.svelte';
  import PageSidebar from './PageSidebar.svelte';

  export let currentRoute = '/';
  export let instanceName = 'AniList-Arr-Sync';
  export let username = '';
  export let isSyncing = false;
  export let isAuthenticated = false;
  export let stagedCount = 0;
  export let reviewCount = 0;
</script>

<div class="page">
  <PageHeader
    {instanceName}
    {username}
    {isSyncing}
    {isAuthenticated}
    on:logout
  />
  <div class="main">
    <PageSidebar
      {currentRoute}
      {stagedCount}
      {reviewCount}
      on:navigate
    />
    <main class="content">
      <slot />
    </main>
  </div>
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    width: 100vw;
    overflow: hidden;
    background-color: var(--pageBackground);
    color: var(--textColor);
    font-family: var(--defaultFontFamily);
  }

  .main {
    display: flex;
    flex: 1 1 auto;
    overflow: hidden;
  }

  .content {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    overflow: hidden;
    position: relative;
  }
</style>
