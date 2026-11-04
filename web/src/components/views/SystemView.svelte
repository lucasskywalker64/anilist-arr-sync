<script lang="ts">
  import { Archive, RefreshCw, Download, RotateCcw, CheckCircle, AlertTriangle } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import FieldSet from '../common/FieldSet.svelte';
  import DescriptionList from '../common/DescriptionList.svelte';
  import Table from '../common/Table.svelte';
  import Button from '../common/Button.svelte';
  import Label from '../common/Label.svelte';
  import Modal from '../common/Modal.svelte';
  import Alert from '../common/Alert.svelte';
  import { api } from '../../lib/api';
  import type { BackupItem, UpdateStatus } from '../../lib/types';

  export let subTab: 'status' | 'backup' | 'updates' = 'status';

  let loading = false;
  let actionLoading = false;
  let errorMsg = '';
  let successMsg = '';

  // Status data
  let health: { status: string; version: string } | null = null;

  // Backup data
  let backups: BackupItem[] = [];
  let restoreModalOpen = false;
  let backupToRestore: BackupItem | null = null;

  // Updates data
  let updateStatus: UpdateStatus | null = null;

  let latestRequestId = 0;

  async function loadData() {
    const requestId = ++latestRequestId;
    loading = true;
    errorMsg = '';
    try {
      if (subTab === 'status') {
        const result = await api.getHealth();
        if (requestId === latestRequestId) health = result;
      } else if (subTab === 'backup') {
        const result = await api.getBackups();
        if (requestId === latestRequestId) backups = result;
      } else if (subTab === 'updates') {
        const result = await api.getUpdateStatus();
        if (requestId === latestRequestId) updateStatus = result;
      }
    } catch (e: any) {
      if (requestId === latestRequestId) {
        errorMsg = e.message || 'Failed to load system data';
      }
    } finally {
      if (requestId === latestRequestId) {
        loading = false;
      }
    }
  }

  async function handleBackupNow() {
    if (actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      const b = await api.createBackup();
      successMsg = `Created backup archive: ${b.filename}`;
      await loadData();
    } catch (e: any) {
      errorMsg = e.message || 'Failed to create backup';
    } finally {
      actionLoading = false;
    }
  }

  function openRestoreModal(item: BackupItem) {
    backupToRestore = item;
    restoreModalOpen = true;
  }

  async function handleConfirmRestore() {
    if (!backupToRestore || actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      await api.restoreBackup(backupToRestore.filename);
      restoreModalOpen = false;
      successMsg = `Database restored successfully from ${backupToRestore.filename}.`;
      backupToRestore = null;
    } catch (e: any) {
      errorMsg = e.message || 'Failed to restore backup';
    } finally {
      actionLoading = false;
    }
  }

  async function handleApplyUpdate() {
    if (actionLoading) return;
    actionLoading = true;
    errorMsg = '';
    successMsg = '';
    try {
      await api.applyUpdate();
      successMsg = 'Update applied successfully. Reloading service...';
    } catch (e: any) {
      errorMsg = e.message || 'Failed to apply update';
    } finally {
      actionLoading = false;
    }
  }

  function formatBytes(bytes: number): string {
    if (!bytes) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
  }

  $: subTab, loadData();
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
      {#if subTab === 'backup'}
        <PageToolbarButton
          label="Backup Now"
          disabled={actionLoading}
          on:click={handleBackupNow}
        >
          <Archive size={18} class={actionLoading ? 'spinning' : ''} />
        </PageToolbarButton>
      {/if}

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

      {#if successMsg}
        <Alert kind="success">{successMsg}</Alert>
      {/if}

      {#if subTab === 'status'}
        <!-- Health FieldSet -->
        <FieldSet legend="Health">
          {#if health}
            <div class="healthBanner">
              <span class="successIcon"><CheckCircle size={24} /></span>
              <span class="healthText">All system health checks passing</span>
            </div>
          {:else if loading}
            <p>Loading health status...</p>
          {/if}
        </FieldSet>

        <!-- About FieldSet using DescriptionList -->
        <FieldSet legend="About">
          <DescriptionList>
            <dt>Version</dt>
            <dd>{health?.version || '1.0.0'}</dd>

            <dt>Application</dt>
            <dd>AniList-Arr-Sync</dd>

            <dt>Database</dt>
            <dd>SQLite3 with WAL mode</dd>

            <dt>API Version</dt>
            <dd>v1 (REST)</dd>

            <dt>Frontend</dt>
            <dd>Svelte SPA (Arr Dark Theme)</dd>
          </DescriptionList>
        </FieldSet>

      {:else if subTab === 'backup'}
        <!-- Backup Archives Table -->
        <FieldSet legend="Database Backups">
          {#if loading && backups.length === 0}
            <p>Loading backups...</p>
          {:else if backups.length === 0}
            <div class="emptyState">
              <h3>No Backup Archives</h3>
              <p>Create a backup to archive current mapping tables, review queue, and configuration.</p>
              <div style="margin-top: 15px;">
                <Button kind="primary" on:click={handleBackupNow} disabled={actionLoading}>
                  <Archive size={16} /> Backup Now
                </Button>
              </div>
            </div>
          {:else}
            <Table>
              <thead>
                <tr>
                  <th>Archive Filename</th>
                  <th>Size</th>
                  <th>Created</th>
                  <th style="text-align: right; width: 120px;">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each backups as item}
                  <tr>
                    <td class="boldName">{item.filename}</td>
                    <td>{formatBytes(item.size)}</td>
                    <td>{new Date(item.createdAt).toLocaleString()}</td>
                    <td style="text-align: right;">
                      <Button kind="warning" size="small" on:click={() => openRestoreModal(item)} disabled={actionLoading}>
                        <RotateCcw size={12} /> Restore
                      </Button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </Table>
          {/if}
        </FieldSet>

      {:else if subTab === 'updates'}
        <!-- Updates -->
        <FieldSet legend="Updates">
          <div class="updateCard">
            <div class="updateInfo">
              <div class="versionRow">
                <span class="versionLabel">Current Version:</span>
                <strong>{updateStatus?.currentVersion || (loading ? 'Loading...' : 'Unknown')}</strong>
              </div>
              <div class="versionRow">
                <span class="versionLabel">Latest Version:</span>
                <strong>{updateStatus?.latestVersion || updateStatus?.currentVersion || (loading ? 'Loading...' : 'Unknown')}</strong>
                {#if updateStatus?.updateAvailable}
                  <Label kind="success" size="small">Update Available</Label>
                {:else if updateStatus}
                  <Label kind="default" size="small">Up to Date</Label>
                {:else}
                  <Label kind="default" size="small">Unknown</Label>
                {/if}
              </div>
              {#if updateStatus?.lastCheckedAt}
                <div class="versionSub">
                  Last checked: {new Date(updateStatus.lastCheckedAt).toLocaleString()}
                </div>
              {/if}
            </div>

            {#if updateStatus?.releaseNotes}
              <div class="notesSection">
                <h4>Release Notes</h4>
                <pre class="notesPre">{updateStatus.releaseNotes}</pre>
              </div>
            {/if}

            {#if updateStatus?.updateAvailable}
              <div style="margin-top: 20px;">
                <Button kind="success" size="medium" on:click={handleApplyUpdate} disabled={actionLoading}>
                  <Download size={16} class={actionLoading ? 'spinning' : ''} /> Install Latest Update
                </Button>
              </div>
            {/if}
          </div>
        </FieldSet>
      {/if}
    </div>
  </div>

  <!-- Restore Confirmation Modal -->
  <Modal
    isOpen={restoreModalOpen}
    title="Restore Database Backup"
    size="medium"
    on:close={() => (restoreModalOpen = false)}
  >
    <div slot="body">
      <p>
        Are you sure you want to restore the database from
        <strong>{backupToRestore?.filename}</strong>?
      </p>
      <Alert kind="warning">
        Restoring will overwrite current mapping overrides, review items, and synchronization history with the archived snapshot.
      </Alert>
    </div>
    <div slot="footer">
      <Button kind="default" on:click={() => (restoreModalOpen = false)} disabled={actionLoading}>
        Cancel
      </Button>
      <Button kind="danger" on:click={handleConfirmRestore} disabled={actionLoading}>
        {actionLoading ? 'Restoring...' : 'Confirm Restore'}
      </Button>
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

  .healthBanner {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 15px 20px;
    background-color: var(--cardBackgroundColor);
    border-radius: 4px;
    border-left: 4px solid var(--successColor);
  }

  .successIcon {
    color: var(--successColor);
  }

  .healthText {
    font-size: var(--defaultFontSize);
    color: var(--textColor);
    font-weight: 500;
  }

  .boldName {
    font-weight: 500;
    color: var(--white);
    font-family: var(--monoSpaceFontFamily);
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

  .updateCard {
    background-color: var(--cardBackgroundColor);
    padding: 20px;
    border-radius: 4px;
    border: 1px solid rgba(255, 255, 255, 0.05);
  }

  .versionRow {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 8px;
    font-size: var(--defaultFontSize);
  }

  .versionLabel {
    color: var(--helpTextColor);
    width: 140px;
  }

  .versionSub {
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
    margin-top: 4px;
  }

  .notesSection {
    margin-top: 20px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    padding-top: 15px;
  }

  .notesSection h4 {
    margin: 0 0 10px 0;
    color: var(--white);
  }

  .notesPre {
    background-color: var(--pageBackground);
    padding: 12px;
    border-radius: 4px;
    font-family: var(--monoSpaceFontFamily);
    font-size: var(--smallFontSize);
    color: var(--textColor);
    white-space: pre-wrap;
    border: 1px solid var(--borderColor);
  }
</style>
