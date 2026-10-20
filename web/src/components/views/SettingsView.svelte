<script lang="ts">
  import { onMount } from 'svelte';
  import { Save, Sliders, Copy, RefreshCw, Plus, Edit, Trash2, Send } from 'lucide-svelte';
  import PageToolbar from '../layout/PageToolbar.svelte';
  import PageToolbarButton from '../layout/PageToolbarButton.svelte';
  import PageContentFooter from '../layout/PageContentFooter.svelte';
  import FieldSet from '../common/FieldSet.svelte';
  import FormGroup from '../common/FormGroup.svelte';
  import Button from '../common/Button.svelte';
  import CheckInput from '../common/CheckInput.svelte';
  import Modal from '../common/Modal.svelte';
  import Alert from '../common/Alert.svelte';
  import Label from '../common/Label.svelte';
  import { api } from '../../lib/api';
  import type { AppConfig, NotificationItem } from '../../lib/types';

  export let subTab: 'general' | 'sync' | 'servarr' | 'connect' = 'general';

  let savedConfig: AppConfig | null = null;
  let formConfig: Partial<AppConfig> = {};
  let notifications: NotificationItem[] = [];

  let showAdvanced = false;
  let loading = false;
  let saving = false;
  let errorMsg = '';
  let successMsg = '';

  // Notifications modal state
  let isNotifModalOpen = false;
  let editingNotif: Partial<NotificationItem> | null = null;
  let notifName = '';
  let notifType: 'DISCORD' | 'WEBHOOK' = 'DISCORD';
  let notifOnSyncStart = false;
  let notifOnSyncSuccess = true;
  let notifOnSyncFailure = true;
  let notifOnReviewNeeded = true;
  let notifWebhookUrl = '';
  let notifTesting = false;
  let notifTestMsg = '';
  let notifError = '';

  $: hasPendingChanges = savedConfig ? JSON.stringify(savedConfig) !== JSON.stringify(formConfig) : false;

  function isFieldDirty(field: keyof AppConfig): boolean {
    if (!savedConfig) return false;
    return savedConfig[field] !== formConfig[field];
  }

  async function loadConfig() {
    loading = true;
    errorMsg = '';
    try {
      savedConfig = await api.getConfig();
      formConfig = JSON.parse(JSON.stringify(savedConfig));
      if (subTab === 'connect') {
        notifications = await api.getNotifications();
      }
    } catch (e: any) {
      errorMsg = e.message || 'Failed to load configuration';
    } finally {
      loading = false;
    }
  }

  async function saveConfig() {
    if (!hasPendingChanges || saving) return;
    saving = true;
    errorMsg = '';
    successMsg = '';
    try {
      await api.updateConfig(formConfig);
      savedConfig = JSON.parse(JSON.stringify(formConfig));
      successMsg = 'Configuration changes saved successfully.';
    } catch (e: any) {
      errorMsg = e.message || 'Failed to save configuration';
    } finally {
      saving = false;
    }
  }

  function resetConfig() {
    if (savedConfig) {
      formConfig = JSON.parse(JSON.stringify(savedConfig));
    }
  }

  function copyApiKey() {
    if (formConfig.apiKey) {
      navigator.clipboard?.writeText(formConfig.apiKey);
      successMsg = 'API key copied to clipboard.';
    }
  }

  function generateApiKey() {
    const chars = 'abcdef0123456789';
    let key = '';
    for (let i = 0; i < 32; i++) {
      key += chars[Math.floor(Math.random() * chars.length)];
    }
    formConfig.apiKey = key;
    formConfig = { ...formConfig };
  }

  // Connect Notifications handlers
  function openAddNotification() {
    editingNotif = null;
    notifName = '';
    notifType = 'DISCORD';
    notifOnSyncStart = false;
    notifOnSyncSuccess = true;
    notifOnSyncFailure = true;
    notifOnReviewNeeded = true;
    notifWebhookUrl = '';
    notifTestMsg = '';
    notifError = '';
    isNotifModalOpen = true;
  }

  function openEditNotification(item: NotificationItem) {
    editingNotif = item;
    notifName = item.name;
    notifType = item.type;
    notifOnSyncStart = item.onSyncStart;
    notifOnSyncSuccess = item.onSyncSuccess;
    notifOnSyncFailure = item.onSyncFailure;
    notifOnReviewNeeded = item.onReviewQueueNeeded;
    notifWebhookUrl = item.settings?.webhookUrl || '';
    notifTestMsg = '';
    notifError = '';
    isNotifModalOpen = true;
  }

  async function testNotification() {
    if (!notifWebhookUrl) {
      notifError = 'Webhook URL is required to test';
      return;
    }
    notifTesting = true;
    notifTestMsg = '';
    notifError = '';
    try {
      const res = await api.testNotificationPayload({
        type: notifType,
        settings: { webhookUrl: notifWebhookUrl },
      });
      notifTestMsg = res.message || 'Test notification sent successfully.';
    } catch (e: any) {
      notifError = e.message || 'Test failed';
    } finally {
      notifTesting = false;
    }
  }

  async function saveNotification() {
    if (!notifName || !notifWebhookUrl) {
      notifError = 'Name and Webhook URL are required';
      return;
    }
    notifError = '';
    try {
      const payload: Partial<NotificationItem> = {
        name: notifName,
        type: notifType,
        onSyncStart: notifOnSyncStart,
        onSyncSuccess: notifOnSyncSuccess,
        onSyncFailure: notifOnSyncFailure,
        onReviewQueueNeeded: notifOnReviewNeeded,
        settings: { webhookUrl: notifWebhookUrl },
      };

      if (editingNotif?.id) {
        await api.updateNotification(editingNotif.id, payload);
      } else {
        await api.createNotification(payload);
      }

      isNotifModalOpen = false;
      notifications = await api.getNotifications();
      successMsg = 'Notification connection saved successfully.';
    } catch (e: any) {
      notifError = e.message || 'Failed to save notification';
    }
  }

  async function deleteNotification(id: number) {
    if (!confirm('Are you sure you want to delete this notification connection?')) return;
    try {
      await api.deleteNotification(id);
      notifications = await api.getNotifications();
      successMsg = 'Notification connection removed.';
    } catch (e: any) {
      errorMsg = e.message || 'Failed to delete notification';
    }
  }

  onMount(() => {
    loadConfig();
  });
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
      <PageToolbarButton
        label={hasPendingChanges ? 'Save Changes' : 'No Changes'}
        disabled={!hasPendingChanges || saving}
        on:click={saveConfig}
      >
        <Save size={18} class={saving ? 'spinning' : ''} />
      </PageToolbarButton>

      <PageToolbarButton
        label="Advanced"
        iconColor={showAdvanced ? 'var(--advancedFormLabelColor)' : undefined}
        on:click={() => (showAdvanced = !showAdvanced)}
      >
        <Sliders size={18} />
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

      {#if subTab === 'general'}
        <!-- Host -->
        <FieldSet legend="Host">
          <FormGroup
            label="Bind Address"
            helpText="Network interface address the HTTP listener binds to (* or 0.0.0.0 for all interfaces)"
            isDirty={isFieldDirty('bindAddress')}
          >
            <input type="text" class="formInput" bind:value={formConfig.bindAddress} />
          </FormGroup>

          <FormGroup
            label="Port Number"
            helpText="Port the internal HTTP web server listens on"
            isDirty={isFieldDirty('port')}
          >
            <input type="number" class="formInput" bind:value={formConfig.port} />
          </FormGroup>

          <FormGroup
            label="URL Base"
            helpText="For reverse proxy subpath support (e.g. /anilist-sync)"
            isDirty={isFieldDirty('urlBase')}
          >
            <input type="text" class="formInput" bind:value={formConfig.urlBase} />
          </FormGroup>

          <FormGroup
            label="Enable SSL"
            helpText="Enables HTTPS listener on configured SSL port with certificates"
            isAdvanced={true}
            isDirty={isFieldDirty('enableSsl')}
          >
            <CheckInput checked={!!formConfig.enableSsl} on:change={(e) => (formConfig.enableSsl = e.detail)} />
          </FormGroup>

          {#if formConfig.enableSsl}
            <FormGroup
              label="SSL Port"
              helpText="Port the internal HTTPS listener listens on"
              isAdvanced={true}
              isDirty={isFieldDirty('sslPort')}
            >
              <input type="number" class="formInput" bind:value={formConfig.sslPort} />
            </FormGroup>

            <FormGroup
              label="SSL Certificate Path"
              helpText="Absolute path to X.509 certificate file"
              isAdvanced={true}
              isDirty={isFieldDirty('sslCertPath')}
            >
              <input type="text" class="formInput" bind:value={formConfig.sslCertPath} />
            </FormGroup>

            <FormGroup
              label="SSL Key Path"
              helpText="Absolute path to certificate private key file"
              isAdvanced={true}
              isDirty={isFieldDirty('sslKeyPath')}
            >
              <input type="text" class="formInput" bind:value={formConfig.sslKeyPath} />
            </FormGroup>
          {/if}
        </FieldSet>

        <!-- Security -->
        <FieldSet legend="Security">
          <FormGroup
            label="Authentication Method"
            helpText="Authentication mode: None, Forms (user/password), or External (reverse proxy headers)"
            isDirty={isFieldDirty('authenticationMethod')}
          >
            <select class="formInput" bind:value={formConfig.authenticationMethod}>
              <option value="None">None</option>
              <option value="Forms">Forms (Login Page)</option>
              <option value="External">External (Authelia / Authentik)</option>
            </select>
          </FormGroup>

          <FormGroup
            label="Authentication Required"
            helpText="Controls whether authentication applies to all clients or is disabled for local LAN addresses"
            isDirty={isFieldDirty('authenticationRequired')}
          >
            <select class="formInput" bind:value={formConfig.authenticationRequired}>
              <option value="Disabled">Disabled</option>
              <option value="Enabled">Enabled (All Clients)</option>
              <option value="DisabledForLocalAddresses">Disabled for Local Addresses</option>
            </select>
          </FormGroup>

          <FormGroup
            label="API Key"
            helpText="Secret key used for API authentication and state-mutating requests"
            isDirty={isFieldDirty('apiKey')}
          >
            <input type="text" class="formInput hasButton" bind:value={formConfig.apiKey} readonly />
            <button type="button" class="formInputButton" on:click={copyApiKey} title="Copy API Key">
              <Copy size={16} />
            </button>
            <button type="button" class="formInputButton" on:click={generateApiKey} title="Generate New Key">
              <RefreshCw size={16} />
            </button>
          </FormGroup>
        </FieldSet>

        <!-- Logging -->
        <FieldSet legend="Logging">
          <FormGroup
            label="File Log Level"
            helpText="Minimum severity level recorded to disk logs"
            isDirty={isFieldDirty('logLevel')}
          >
            <select class="formInput" bind:value={formConfig.logLevel}>
              <option value="Trace">Trace</option>
              <option value="Debug">Debug</option>
              <option value="Info">Info</option>
              <option value="Warn">Warn</option>
              <option value="Error">Error</option>
            </select>
          </FormGroup>

          <FormGroup
            label="Console Log Level"
            helpText="Minimum severity level written to standard output"
            isAdvanced={true}
            isDirty={isFieldDirty('consoleLogLevel')}
          >
            <select class="formInput" bind:value={formConfig.consoleLogLevel}>
              <option value="Trace">Trace</option>
              <option value="Debug">Debug</option>
              <option value="Info">Info</option>
              <option value="Warn">Warn</option>
              <option value="Error">Error</option>
            </select>
          </FormGroup>
        </FieldSet>

        <!-- Instance -->
        <FieldSet legend="Instance">
          <FormGroup
            label="Instance Name"
            helpText="Display name rendered in header and page titles"
            isDirty={isFieldDirty('instanceName')}
          >
            <input type="text" class="formInput" bind:value={formConfig.instanceName} />
          </FormGroup>
        </FieldSet>

      {:else if subTab === 'sync'}
        <!-- AniList & Sync Settings -->
        <FieldSet legend="AniList Configuration">
          <FormGroup
            label="AniList Username"
            helpText="The AniList user account to sync anime library lists from"
            isDirty={isFieldDirty('aniListUsername')}
          >
            <input type="text" class="formInput" placeholder="e.g. animelover99" bind:value={formConfig.aniListUsername} />
          </FormGroup>

          <FormGroup
            label="Sync Interval (Hours)"
            helpText="Interval in hours between automated background synchronization cycles"
            isDirty={isFieldDirty('syncIntervalHours')}
          >
            <input type="number" class="formInput" min="1" max="168" bind:value={formConfig.syncIntervalHours} />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Matching Rules & Policy">
          <FormGroup
            label="Date Tolerance (Days)"
            helpText="Maximum allowable divergence in release dates when matching entries"
            isAdvanced={true}
            isDirty={isFieldDirty('dateToleranceDays')}
          >
            <input type="number" class="formInput" min="0" max="365" bind:value={formConfig.dateToleranceDays} />
          </FormGroup>

          <FormGroup
            label="Confidence Threshold"
            helpText="Minimum fuzzy match score (0.0 - 1.0) required to auto-bind without review diversion"
            isAdvanced={true}
            isDirty={isFieldDirty('confidenceThreshold')}
          >
            <input type="number" step="0.05" min="0" max="1" class="formInput" bind:value={formConfig.confidenceThreshold} />
          </FormGroup>

          <FormGroup
            label="Unmonitor Dropped"
            helpText="Automatically unmonitor media in Sonarr/Radarr when dropped on AniList"
            isDirty={isFieldDirty('unmonitorDropped')}
          >
            <CheckInput checked={!!formConfig.unmonitorDropped} on:change={(e) => (formConfig.unmonitorDropped = e.detail)} />
          </FormGroup>

          <FormGroup
            label="First Run Dry Run"
            helpText="Stage all actions for approval on the first sync cycle instead of applying immediately"
            isDirty={isFieldDirty('firstRunDryRun')}
          >
            <CheckInput checked={!!formConfig.firstRunDryRun} on:change={(e) => (formConfig.firstRunDryRun = e.detail)} />
          </FormGroup>
        </FieldSet>

      {:else if subTab === 'servarr'}
        <!-- Servarr -->
        <FieldSet legend="Sonarr">
          <FormGroup
            label="Sonarr URL"
            helpText="Full base URL to your Sonarr v3/v4 server (e.g. http://localhost:8989)"
            isDirty={isFieldDirty('sonarrUrl')}
          >
            <input type="text" class="formInput" placeholder="http://localhost:8989" bind:value={formConfig.sonarrUrl} />
          </FormGroup>

          <FormGroup
            label="Sonarr API Key"
            helpText="API key from Sonarr -> Settings -> General -> Security"
            isDirty={isFieldDirty('sonarrApiKey')}
          >
            <input type="text" class="formInput" bind:value={formConfig.sonarrApiKey} />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Radarr">
          <FormGroup
            label="Radarr URL"
            helpText="Full base URL to your Radarr v3/v4/v5 server (e.g. http://localhost:7878)"
            isDirty={isFieldDirty('radarrUrl')}
          >
            <input type="text" class="formInput" placeholder="http://localhost:7878" bind:value={formConfig.radarrUrl} />
          </FormGroup>

          <FormGroup
            label="Radarr API Key"
            helpText="API key from Radarr -> Settings -> General -> Security"
            isDirty={isFieldDirty('radarrApiKey')}
          >
            <input type="text" class="formInput" bind:value={formConfig.radarrApiKey} />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Tagging">
          <FormGroup
            label="Arr Tag Name"
            helpText="Tag applied to items managed by AniList sync in Sonarr and Radarr"
            isDirty={isFieldDirty('tagName')}
          >
            <input type="text" class="formInput" placeholder="anilist" bind:value={formConfig.tagName} />
          </FormGroup>
        </FieldSet>

      {:else if subTab === 'connect'}
        <!-- Connect Notifications -->
        <FieldSet legend="Notification Connections">
          <div class="notificationsGrid">
            {#each notifications as notif (notif.id)}
              <div class="notificationCard">
                <div class="notifHeader">
                  <span class="notifName">{notif.name}</span>
                  <Label kind="primary" size="small">{notif.type}</Label>
                </div>
                <div class="notifUrl">{notif.settings?.webhookUrl || 'Webhook URL configured'}</div>
                <div class="notifTriggers">
                  {#if notif.onSyncStart}<Label kind="default" size="small">Sync Start</Label>{/if}
                  {#if notif.onSyncSuccess}<Label kind="success" size="small">Success</Label>{/if}
                  {#if notif.onSyncFailure}<Label kind="danger" size="small">Failure</Label>{/if}
                  {#if notif.onReviewQueueNeeded}<Label kind="warning" size="small">Review Needed</Label>{/if}
                </div>
                <div class="notifActions">
                  <button class="notifActionBtn" on:click={() => openEditNotification(notif)} title="Edit">
                    <Edit size={16} />
                  </button>
                  <button class="notifActionBtn deleteBtn" on:click={() => deleteNotification(notif.id)} title="Delete">
                    <Trash2 size={16} />
                  </button>
                </div>
              </div>
            {/each}

            <!-- 290px Add Notification Card -->
            <button type="button" class="addNotificationCard" on:click={openAddNotification}>
              <div class="centerPlus">
                <Plus size={40} />
              </div>
              <span class="addLabel">Add Notification Connection</span>
            </button>
          </div>
        </FieldSet>
      {/if}
    </div>
  </div>

  <!-- Sticky Unsaved Changes Save Bar -->
  {#if hasPendingChanges}
    <PageContentFooter>
      <div class="stickySaveText">
        <span>Unsaved configuration changes</span>
      </div>
      <div class="stickySaveActions">
        <Button kind="default" size="medium" on:click={resetConfig} disabled={saving}>
          Reset
        </Button>
        <Button kind="primary" size="medium" on:click={saveConfig} disabled={saving}>
          <Save size={16} class={saving ? 'spinning' : ''} /> Save Changes
        </Button>
      </div>
    </PageContentFooter>
  {/if}

  <!-- Add / Edit Notification Connection Modal -->
  <Modal
    isOpen={isNotifModalOpen}
    title={editingNotif ? `Edit ${editingNotif.name}` : 'Add Notification Connection'}
    size="medium"
    on:close={() => (isNotifModalOpen = false)}
  >
    <div slot="body">
      {#if notifError}
        <Alert kind="danger">{notifError}</Alert>
      {/if}
      {#if notifTestMsg}
        <Alert kind="success">{notifTestMsg}</Alert>
      {/if}

      <FormGroup label="Name" helpText="Display name for this notification connection">
        <input type="text" class="formInput" placeholder="Discord Channel" bind:value={notifName} />
      </FormGroup>

      <FormGroup label="Provider Type" helpText="Webhook payload format">
        <select class="formInput" bind:value={notifType}>
          <option value="DISCORD">Discord Webhook</option>
          <option value="WEBHOOK">Generic Webhook</option>
        </select>
      </FormGroup>

      <FormGroup label="Webhook URL" helpText="HTTP URL to deliver notifications to">
        <input type="text" class="formInput" placeholder="https://discord.com/api/webhooks/..." bind:value={notifWebhookUrl} />
      </FormGroup>

      <FormGroup label="On Sync Start" helpText="Notify when a synchronization cycle starts">
        <CheckInput checked={notifOnSyncStart} on:change={(e) => (notifOnSyncStart = e.detail)} />
      </FormGroup>

      <FormGroup label="On Sync Success" helpText="Notify when synchronization completes successfully">
        <CheckInput checked={notifOnSyncSuccess} on:change={(e) => (notifOnSyncSuccess = e.detail)} />
      </FormGroup>

      <FormGroup label="On Sync Failure" helpText="Notify when synchronization fails or encounters errors">
        <CheckInput checked={notifOnSyncFailure} on:change={(e) => (notifOnSyncFailure = e.detail)} />
      </FormGroup>

      <FormGroup label="On Review Needed" helpText="Notify when items are diverted to the review queue">
        <CheckInput checked={notifOnReviewNeeded} on:change={(e) => (notifOnReviewNeeded = e.detail)} />
      </FormGroup>
    </div>

    <div slot="footer">
      <Button kind="default" on:click={testNotification} disabled={notifTesting}>
        <Send size={14} class={notifTesting ? 'spinning' : ''} /> Test
      </Button>
      <Button kind="default" on:click={() => (isNotifModalOpen = false)}>
        Cancel
      </Button>
      <Button kind="primary" on:click={saveNotification}>
        Save Connection
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

  .stickySaveText {
    color: var(--warningColor);
    font-weight: 500;
    font-size: var(--defaultFontSize);
  }

  .stickySaveActions {
    display: flex;
    gap: 12px;
  }

  /* Connect Notification Cards */
  .notificationsGrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
    gap: 20px;
    margin-top: 15px;
  }

  .notificationCard {
    width: 290px;
    min-height: 180px;
    padding: 20px;
    background-color: var(--cardBackgroundColor);
    border-radius: 4px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    border: 1px solid rgba(255, 255, 255, 0.05);
  }

  .notifHeader {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .notifName {
    font-size: 16px;
    font-weight: 600;
    color: var(--white);
  }

  .notifUrl {
    font-size: var(--smallFontSize);
    color: var(--helpTextColor);
    word-break: break-all;
    margin-bottom: 12px;
  }

  .notifTriggers {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 15px;
  }

  .notifActions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    padding-top: 10px;
  }

  .notifActionBtn {
    background: transparent;
    border: none;
    color: var(--textColor);
    cursor: pointer;
    padding: 4px;
    border-radius: 3px;
    transition: color 150ms ease;
  }

  .notifActionBtn:hover {
    color: var(--themeBlue);
  }

  .deleteBtn:hover {
    color: var(--dangerColor);
  }

  .addNotificationCard {
    width: 290px;
    min-height: 180px;
    background-color: var(--cardCenterBackgroundColor);
    border: 2px dashed var(--borderColor);
    border-radius: 4px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: background-color 200ms ease, border-color 200ms ease;
    padding: 20px;
  }

  .addNotificationCard:hover {
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
    border-color: var(--sonarrBlue);
  }

  .centerPlus {
    color: var(--sonarrBlue);
    margin-bottom: 10px;
  }

  .addLabel {
    font-size: var(--defaultFontSize);
    color: var(--textColor);
    text-align: center;
  }
</style>
