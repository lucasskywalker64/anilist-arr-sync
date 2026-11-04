<script lang="ts">
  import { onMount } from 'svelte';
  import { RefreshCw, FileText } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Modal from '../common/Modal.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { SyncHistoryItem } from '../../lib/types';

  let historyItems: SyncHistoryItem[] = [];
  let loading = false;
  let errorMsg = '';

  // Error modal
  let selectedLog: SyncHistoryItem | null = null;
  let isLogModalOpen = false;

  function viewLog(item: SyncHistoryItem) {
    selectedLog = item;
    isLogModalOpen = true;
  }

  function formatDuration(ms: number): string {
    if (!ms || ms < 1000) return `${ms || 0}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function formatTrigger(trigger: string): string {
    switch (trigger) {
      case 'SCHEDULED': return 'Scheduled';
      case 'MANUAL_WEB': return 'Manual';
      case 'CLI': return 'CLI';
      default: return trigger;
    }
  }

  function parseErrors(errorsJson?: string | null): string[] {
    if (!errorsJson) return [];
    try {
      const parsed = JSON.parse(errorsJson);
      if (Array.isArray(parsed)) return parsed;
      return [String(parsed)];
    } catch {
      return [errorsJson];
    }
  }

  async function loadData() {
    loading = true;
    errorMsg = '';
    try {
      historyItems = await api.getSyncHistory();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load sync history';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
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

      {#if historyItems.length === 0}
        <div class="emptyState">
          <h3>No Synchronization History</h3>
          <p>No historical synchronization cycles have run on this instance yet.</p>
        </div>
      {:else}
        <Table>
          <thead>
            <tr>
              <th>Status</th>
              <th>Trigger</th>
              <th>Timestamp</th>
              <th>Duration</th>
              <th>Scanned</th>
              <th>Sonarr</th>
              <th>Radarr</th>
              <th>Review Queue</th>
              <th style="text-align: right;">Details</th>
            </tr>
          </thead>
          <tbody>
            {#each historyItems as item (item.id)}
              <tr>
                <td>
                  <Label
                    kind={item.status === 'SUCCESS' ? 'success' : item.status === 'FAILED' ? 'danger' : 'warning'}
                    size="small"
                  >
                    {item.status}
                  </Label>
                </td>
                <td>
                  <span class="triggerBadge">{formatTrigger(item.triggerType)}</span>
                </td>
                <td>{item.runTimestamp ? new Date(item.runTimestamp).toLocaleString() : '-'}</td>
                <td>{formatDuration(item.durationMs)}</td>
                <td>{item.itemsScanned}</td>
                <td>
                  <div class="countDelta">
                    {#if item.monitoredSonarr > 0}
                      <span class="addCount">+{item.monitoredSonarr}</span>
                    {/if}
                    {#if item.unmonitoredSonarr > 0}
                      <span class="removeCount">-{item.unmonitoredSonarr}</span>
                    {/if}
                    {#if item.monitoredSonarr === 0 && item.unmonitoredSonarr === 0}
                      <span class="muted">0</span>
                    {/if}
                  </div>
                </td>
                <td>
                  <div class="countDelta">
                    {#if item.addedRadarr > 0}
                      <span class="addCount">+{item.addedRadarr}</span>
                    {/if}
                    {#if item.unmonitoredRadarr > 0}
                      <span class="removeCount">-{item.unmonitoredRadarr}</span>
                    {/if}
                    {#if item.addedRadarr === 0 && item.unmonitoredRadarr === 0}
                      <span class="muted">0</span>
                    {/if}
                  </div>
                </td>
                <td>
                  {#if item.queuedReview > 0}
                    <span class="reviewQueued">{item.queuedReview}</span>
                  {:else}
                    <span class="muted">0</span>
                  {/if}
                </td>
                <td style="text-align: right;">
                  {#if parseErrors(item.errorsJson).length > 0}
                    <Button kind="default" size="small" on:click={() => viewLog(item)}>
                      <FileText size={12} /> Log ({parseErrors(item.errorsJson).length})
                    </Button>
                  {:else}
                    <span class="muted">-</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </div>
  </div>

  <!-- Error / Details Modal -->
  <Modal
    isOpen={isLogModalOpen}
    title="Synchronization Execution Log #{selectedLog?.id}"
    size="medium"
    on:close={() => (isLogModalOpen = false)}
  >
    <div slot="body">
      {#if selectedLog && parseErrors(selectedLog.errorsJson).length > 0}
        <div class="logErrorsList">
          {#each parseErrors(selectedLog.errorsJson) as err}
            <pre class="logPre">{err}</pre>
          {/each}
        </div>
      {:else}
        <p>No error details recorded for this run.</p>
      {/if}
    </div>
    <div slot="footer">
      <Button kind="default" on:click={() => (isLogModalOpen = false)}>Close</Button>
    </div>
  </Modal>
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

  .emptyState {
    text-align: center;
    padding: 60px 20px;
    color: var(--disabledColor);
  }

  .emptyState h3 {
    color: var(--textColor);
    font-size: 20px;
    margin-bottom: 8px;
  }

  .triggerBadge {
    color: var(--textColor);
    font-size: var(--smallFontSize);
  }

  .countDelta {
    display: inline-flex;
    gap: 6px;
    align-items: center;
  }

  .addCount {
    color: var(--successColor);
  }

  .removeCount {
    color: var(--warningColor);
  }

  .reviewQueued {
    color: var(--warningColor);
    font-weight: 600;
  }

  .muted {
    color: var(--disabledColor);
  }

  .logErrorsList {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .logPre {
    background-color: var(--pageBackground);
    padding: 12px;
    border-radius: 4px;
    color: var(--dangerColor);
    font-family: var(--monoSpaceFontFamily);
    font-size: var(--smallFontSize);
    overflow-x: auto;
    white-space: pre-wrap;
    max-height: 250px;
    border: 1px solid var(--borderColor);
    margin: 0;
  }
</style>
