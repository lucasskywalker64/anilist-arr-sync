<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import { RefreshCw, Play, AlertTriangle, Layers, ListFilter, CheckCircle, XCircle } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Card from '../common/Card.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Alert from '../common/Alert.svelte';
  import FieldSet from '../common/FieldSet.svelte';
  import DescriptionList from '../common/DescriptionList.svelte';
  import { api } from '../../lib/api';
  import type { SyncStatus, ReviewItem, StagedAction, OverrideItem } from '../../lib/types';

  export let isSyncing = false;
  export let reviewItems: ReviewItem[] = [];
  export let stagedActions: StagedAction[] = [];
  export let overrides: OverrideItem[] = [];

  const dispatch = createEventDispatcher<{
    navigate: string;
    refresh: void;
  }>();

  let syncStatus: SyncStatus | null = null;
  let loading = false;
  let startingSync = false;
  let errorMsg = '';
  let triggerMsg = '';

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      syncStatus = await api.getSyncStatus();
      isSyncing = syncStatus.isSyncing;
      dispatch('refresh');
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load sync status';
    } finally {
      loading = false;
    }
  }

  async function triggerSync() {
    if (isSyncing || startingSync) return;
    startingSync = true;
    triggerMsg = '';
    errorMsg = '';
    try {
      await api.startSync();
      isSyncing = true;
      triggerMsg = 'Sync started successfully.';
      setTimeout(loadData, 1000);
    } catch (e: any) {
      errorMsg = e.message || 'Failed to start sync';
    } finally {
      startingSync = false;
    }
  }

  function formatDuration(ms?: number): string {
    if (!ms || ms < 1000) return `${ms || 0}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function formatTrigger(trigger?: string): string {
    switch (trigger) {
      case 'SCHEDULED': return 'Scheduled';
      case 'MANUAL_WEB': return 'Manual';
      case 'CLI': return 'CLI';
      default: return trigger || 'Manual';
    }
  }

  onMount(() => {
    loadData();
    const interval = setInterval(() => {
      if (isSyncing) {
        loadData();
      }
    }, 3000);
    return () => clearInterval(interval);
  });
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
      <PageToolbarButton
        label="Sync Now"
        disabled={isSyncing || startingSync}
        on:click={triggerSync}
      >
        <Play size={18} class={isSyncing ? 'spinning' : ''} />
      </PageToolbarButton>

      <PageToolbarButton
        label="Refresh"
        disabled={loading}
        on:click={loadData}
      >
        <RefreshCw size={18} class={loading ? 'spinning' : ''} />
      </PageToolbarButton>
    </svelte:fragment>
  </PageToolbar>

  <div class="body">
    <div class="innerContentBody">
      {#if errorMsg}
        <Alert kind="danger">{errorMsg}</Alert>
      {/if}

      {#if triggerMsg}
        <Alert kind="success">{triggerMsg}</Alert>
      {/if}

      <!-- Status Banner -->
      <Card>
        <div class="statusBanner">
          <div class="statusIcon">
            {#if isSyncing}
              <RefreshCw size={32} class="spinning syncColor" />
            {:else if syncStatus?.lastSync?.status === 'SUCCESS'}
              <CheckCircle size={32} class="successColor" />
            {:else if syncStatus?.lastSync?.status === 'FAILED'}
              <XCircle size={32} class="dangerColor" />
            {:else}
              <RefreshCw size={32} class="primaryColor" />
            {/if}
          </div>
          <div class="statusDetails">
            <h2 class="statusTitle">
              {isSyncing ? 'Synchronization in Progress' : 'System Idle'}
            </h2>
            <div class="statusSub">
              {#if isSyncing}
                <span>Processing AniList library entries and comparing against Servarr...</span>
              {:else if syncStatus?.lastSync}
                <span>
                  Last synchronized: {syncStatus.lastSync.startTime ? new Date(syncStatus.lastSync.startTime).toLocaleString() : 'Recently'}
                  {#if syncStatus.lastSync.durationMs}
                    ({formatDuration(syncStatus.lastSync.durationMs)})
                  {/if}
                </span>
                <span class="statusBadge">
                  <Label
                    kind={syncStatus.lastSync.status === 'SUCCESS' ? 'success' : syncStatus.lastSync.status === 'FAILED' ? 'danger' : 'warning'}
                    size="medium"
                  >
                    {syncStatus.lastSync.status}
                  </Label>
                </span>
              {:else}
                <span>No synchronization runs recorded yet.</span>
              {/if}
            </div>
          </div>
        </div>
      </Card>

      <!-- Metric Summary Cards -->
      <div class="metricsGrid">
        <button
          type="button"
          class="metricCard"
          on:click={() => dispatch('navigate', '/activity/review')}
        >
          <div class="metricIcon warningBg">
            <AlertTriangle size={24} />
          </div>
          <div class="metricInfo">
            <span class="metricCount">{reviewItems.length}</span>
            <span class="metricLabel">Review Queue</span>
          </div>
        </button>

        <button
          type="button"
          class="metricCard"
          on:click={() => dispatch('navigate', '/staged')}
        >
          <div class="metricIcon infoBg">
            <Layers size={24} />
          </div>
          <div class="metricInfo">
            <span class="metricCount">{stagedActions.length}</span>
            <span class="metricLabel">Staged Actions</span>
          </div>
        </button>

        <button
          type="button"
          class="metricCard"
          on:click={() => dispatch('navigate', '/overrides')}
        >
          <div class="metricIcon primaryBg">
            <ListFilter size={24} />
          </div>
          <div class="metricInfo">
            <span class="metricCount">{overrides.length}</span>
            <span class="metricLabel">Overrides</span>
          </div>
        </button>
      </div>

      <!-- Last Execution Details -->
      {#if syncStatus?.lastSync}
        <FieldSet legend="Last Execution Summary">
          <DescriptionList>
            <dt>Executed At</dt>
            <dd>{syncStatus.lastSync.startTime ? new Date(syncStatus.lastSync.startTime).toLocaleString() : '-'}</dd>

            <dt>Duration</dt>
            <dd>{formatDuration(syncStatus.lastSync.durationMs)}</dd>

            <dt>Trigger</dt>
            <dd>{formatTrigger(syncStatus.lastSync.trigger)}</dd>

            <dt>Items Scanned</dt>
            <dd>{syncStatus.lastSync.itemsScanned}</dd>

            <dt>Sonarr Updates</dt>
            <dd>+{syncStatus.lastSync.monitoredSonarr} monitored, -{syncStatus.lastSync.unmonitoredSonarr} unmonitored</dd>

            <dt>Radarr Updates</dt>
            <dd>+{syncStatus.lastSync.addedRadarr} added, -{syncStatus.lastSync.unmonitoredRadarr} unmonitored</dd>

            <dt>Review Queue</dt>
            <dd>{syncStatus.lastSync.queuedReview} queued</dd>

            {#if syncStatus.lastSync.errors && syncStatus.lastSync.errors.length > 0}
              <dt>Errors</dt>
              <dd>
                {#each syncStatus.lastSync.errors as err}
                  <div class="errorItem">{err}</div>
                {/each}
              </dd>
            {/if}
          </DescriptionList>
        </FieldSet>
      {/if}
    </div>
  </div>
</div>

<style>
  .viewContainer {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    overflow: hidden;
  }

  .body {
    flex: 1 0 1px;
    overflow-y: auto;
  }

  .innerContentBody {
    padding: var(--pageContentBodyPadding);
  }

  .statusBanner {
    display: flex;
    align-items: center;
    gap: 20px;
  }

  .statusTitle {
    margin: 0 0 6px 0;
    font-size: 20px;
    font-weight: 400;
    color: var(--white);
  }

  .statusSub {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--helpTextColor);
    font-size: var(--defaultFontSize);
  }

  :global(.syncColor) {
    color: var(--sonarrBlue);
  }

  :global(.successColor) {
    color: var(--successColor);
  }

  :global(.dangerColor) {
    color: var(--dangerColor);
  }

  :global(.primaryColor) {
    color: var(--primaryColor);
  }

  .metricsGrid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 20px;
    margin: 20px 0;
  }

  .metricCard {
    display: flex;
    align-items: center;
    gap: 15px;
    padding: 18px 20px;
    background-color: var(--cardBackgroundColor);
    border: none;
    border-radius: 4px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
    cursor: pointer;
    font-family: inherit;
    text-align: left;
    transition: transform 150ms ease, background-color 150ms ease;
  }

  .metricCard:hover {
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
    transform: translateY(-2px);
  }

  .metricIcon {
    width: 48px;
    height: 48px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--white);
  }

  .warningBg {
    background-color: var(--warningColor);
  }

  .infoBg {
    background-color: var(--infoColor);
  }

  .primaryBg {
    background-color: var(--purple);
  }

  .metricInfo {
    display: flex;
    flex-direction: column;
  }

  .metricCount {
    font-size: 26px;
    font-weight: 600;
    line-height: 1.1;
    color: var(--white);
  }

  .metricLabel {
    font-size: var(--defaultFontSize);
    color: var(--helpTextColor);
  }

  .errorItem {
    color: var(--dangerColor);
    font-family: var(--monoSpaceFontFamily);
    font-size: var(--smallFontSize);
    margin-bottom: 4px;
  }
</style>
