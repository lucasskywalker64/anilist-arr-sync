<script lang="ts">
  import { onMount } from "svelte";
  import {
    Save,
    Sliders,
    Copy,
    Plus,
    Edit,
    Trash2,
    Send,
    RefreshCw,
    Folder,
    Webhook,
    ArrowLeft,
  } from "lucide-svelte";
  import PageToolbar from "../layout/PageToolbar.svelte";
  import PageToolbarButton from "../layout/PageToolbarButton.svelte";
  import PageContentFooter from "../layout/PageContentFooter.svelte";
  import FieldSet from "../common/FieldSet.svelte";
  import FormGroup from "../common/FormGroup.svelte";
  import Button from "../common/Button.svelte";
  import CheckInput from "../common/CheckInput.svelte";
  import Modal from "../common/Modal.svelte";
  import FolderBrowserModal from "../common/FolderBrowserModal.svelte";
  import Alert from "../common/Alert.svelte";
  import Label from "../common/Label.svelte";
  import { api } from "../../lib/api";
  import type { AppConfig, NotificationItem } from "../../lib/types";

  export let subTab: "general" | "sync" | "servarr" | "connect" = "general";

  let savedConfig: AppConfig | null = null;
  let formConfig: Partial<AppConfig> = {};
  let notifications: NotificationItem[] = [];

  let formPassword = "";
  let formPasswordConfirm = "";

  let showAdvanced = false;
  let loading = false;
  let saving = false;
  let errorMsg = "";
  let successMsg = "";

  // Notifications modal state
  let isNotifModalOpen = false;
  let isFolderBrowserOpen = false;
  let notifModalStep: "select" | "edit" = "select";
  let editingNotif: Partial<NotificationItem> | null = null;
  let notifName = "";
  let notifType: "DISCORD" | "WEBHOOK" = "DISCORD";
  let notifOnAdded = true;
  let notifOnReview = true;
  let notifOnError = true;
  let notifOnComplete = false;
  let notifOnUpdate = true;
  let notifWebhookUrl = "";
  let notifTesting = false;
  let notifSaving = false;
  let notifTestMsg = "";
  let notifError = "";

  $: notifModalTitle = editingNotif
    ? `Edit Notification - ${editingNotif.name || (notifType === "DISCORD" ? "Discord" : "Webhook")}`
    : notifModalStep === "select"
      ? "Add Notification"
      : `Add Notification - ${notifType === "DISCORD" ? "Discord" : "Webhook"}`;

  $: hasPendingChanges =
    (savedConfig
      ? JSON.stringify(savedConfig) !== JSON.stringify(formConfig)
      : false) || !!formPassword;

  function isFieldDirty(field: keyof AppConfig): boolean {
    if (!savedConfig) return false;
    return savedConfig[field] !== formConfig[field];
  }

  async function loadNotifications() {
    try {
      notifications = await api.getNotifications();
    } catch (e: any) {
      errorMsg = e.message || "Failed to load notifications";
    }
  }

  $: if (subTab === "connect") {
    loadNotifications();
  }

  async function loadConfig() {
    loading = true;
    errorMsg = "";
    try {
      savedConfig = await api.getConfig();
      formConfig = JSON.parse(JSON.stringify(savedConfig));
      if (subTab === "connect") {
        await loadNotifications();
      }
    } catch (e: any) {
      errorMsg = e.message || "Failed to load configuration";
    } finally {
      loading = false;
    }
  }

  async function saveConfig() {
    if (!hasPendingChanges || saving) return;
    if (formPassword || formPasswordConfirm) {
      if (formPassword !== formPasswordConfirm) {
        errorMsg = "Passwords do not match";
        return;
      }
    }
    saving = true;
    errorMsg = "";
    successMsg = "";
    try {
      const payload: Partial<AppConfig> = { ...formConfig };
      if (formPassword) {
        payload.password = formPassword;
      }
      await api.updateConfig(payload);
      savedConfig = JSON.parse(JSON.stringify(formConfig));
      formPassword = "";
      formPasswordConfirm = "";
      successMsg = "Configuration changes saved successfully.";
    } catch (e: any) {
      errorMsg = e.message || "Failed to save configuration";
    } finally {
      saving = false;
    }
  }

  function resetConfig() {
    if (savedConfig) {
      formConfig = JSON.parse(JSON.stringify(savedConfig));
      formPassword = "";
      formPasswordConfirm = "";
    }
  }

  function copyApiKey() {
    if (formConfig.apiKey) {
      navigator.clipboard?.writeText(formConfig.apiKey);
      successMsg = "API key copied to clipboard.";
    }
  }

  function resetApiKey() {
    if (
      confirm(
        "Resetting the API Key will invalidate the current key and require all external integrations to be updated. Continue?",
      )
    ) {
      const arr = new Uint8Array(16);
      crypto.getRandomValues(arr);
      formConfig.apiKey = Array.from(arr, (b) =>
        b.toString(16).padStart(2, "0"),
      ).join("");
      successMsg = 'New API key generated. Click "Save Changes" to apply.';
    }
  }

  // Connect Notifications handlers
  function openAddNotification() {
    editingNotif = null;
    notifModalStep = "select";
    notifName = "";
    notifType = "DISCORD";
    notifOnAdded = true;
    notifOnReview = true;
    notifOnError = true;
    notifOnComplete = false;
    notifOnUpdate = true;
    notifWebhookUrl = "";
    notifTestMsg = "";
    notifError = "";
    isNotifModalOpen = true;
  }

  function selectConnectionType(type: "DISCORD" | "WEBHOOK") {
    notifType = type;
    notifName = type === "DISCORD" ? "Discord" : "Webhook";
    notifWebhookUrl = "";
    notifOnAdded = true;
    notifOnReview = true;
    notifOnError = true;
    notifOnComplete = false;
    notifOnUpdate = true;
    notifTestMsg = "";
    notifError = "";
    notifModalStep = "edit";
  }

  function openEditNotification(item: NotificationItem) {
    editingNotif = item;
    notifModalStep = "edit";
    notifName = item.name;
    notifType = (item.provider || item.type || "DISCORD").toUpperCase() as
      | "DISCORD"
      | "WEBHOOK";
    notifOnAdded = item.onAdded ?? item.onSyncStart ?? true;
    notifOnReview = item.onReview ?? item.onReviewQueueNeeded ?? true;
    notifOnError = item.onError ?? item.onSyncFailure ?? true;
    notifOnComplete = item.onComplete ?? item.onSyncSuccess ?? false;
    notifOnUpdate = item.onUpdate ?? true;
    notifWebhookUrl =
      typeof item.settings?.webhookUrl === "string"
        ? item.settings.webhookUrl
        : typeof item.settings?.url === "string"
          ? item.settings.url
          : "";
    notifTestMsg = "";
    notifError = "";
    isNotifModalOpen = true;
  }

  async function testNotification() {
    if (!notifWebhookUrl.trim()) {
      notifError = "Webhook URL is required to test";
      return;
    }
    notifTesting = true;
    notifTestMsg = "";
    notifError = "";
    try {
      const res = await api.testNotificationPayload({
        type: notifType,
        settings: {
          webhookUrl: notifWebhookUrl.trim(),
          url: notifWebhookUrl.trim(),
        },
      });
      notifTestMsg = res.message || "Test notification sent successfully.";
    } catch (e: any) {
      notifError = e.message || "Test failed";
    } finally {
      notifTesting = false;
    }
  }

  async function saveNotification() {
    if (notifSaving) return;
    if (!notifName.trim()) {
      notifError = "Name is required";
      return;
    }
    if (!notifWebhookUrl.trim()) {
      notifError = "Webhook URL is required";
      return;
    }
    notifError = "";
    notifSaving = true;
    try {
      let existingSettings: Record<string, any> = {};
      if (editingNotif?.settings && typeof editingNotif.settings === 'object') {
        existingSettings = { ...editingNotif.settings };
      } else if (editingNotif?.configJson) {
        try {
          existingSettings = JSON.parse(editingNotif.configJson);
        } catch {
          // ignore invalid JSON
        }
      }
      const updatedSettings = {
        ...existingSettings,
        webhookUrl: notifWebhookUrl.trim(),
        url: notifWebhookUrl.trim(),
      };

      const payload: Partial<NotificationItem> = {
        name: notifName.trim(),
        type: notifType,
        provider: notifType,
        onAdded: notifOnAdded,
        onReview: notifOnReview,
        onError: notifOnError,
        onComplete: notifOnComplete,
        onUpdate: notifOnUpdate,
        onSyncStart: notifOnAdded,
        onSyncSuccess: notifOnComplete,
        onSyncFailure: notifOnError,
        onReviewQueueNeeded: notifOnReview,
        configJson: JSON.stringify(updatedSettings),
        settings: updatedSettings,
      };

      if (editingNotif?.id) {
        await api.updateNotification(editingNotif.id, payload);
      } else {
        await api.createNotification(payload);
      }

      isNotifModalOpen = false;
      notifications = await api.getNotifications();
      successMsg = "Notification connection saved successfully.";
    } catch (e: any) {
      notifError = e.message || "Failed to save notification";
    } finally {
      notifSaving = false;
    }
  }

  async function deleteNotification(id: number) {
    if (
      !confirm("Are you sure you want to delete this notification connection?")
    )
      return;
    try {
      await api.deleteNotification(id);
      notifications = await api.getNotifications();
      successMsg = "Notification connection removed.";
    } catch (e: any) {
      errorMsg = e.message || "Failed to delete notification";
    }
  }

  $: if (
    formConfig.updateMechanism &&
    formConfig.updateMechanism !== "BuiltIn" &&
    formConfig.updateAutomatically
  ) {
    formConfig.updateAutomatically = false;
  }

  onMount(() => {
    loadConfig();
  });
</script>

<div class="viewContainer">
  <PageToolbar>
    <svelte:fragment slot="left">
      <PageToolbarButton
        label={hasPendingChanges ? "Save Changes" : "No Changes"}
        disabled={!hasPendingChanges || saving}
        on:click={saveConfig}
      >
        <Save size={18} class={saving ? "spinning" : ""} />
      </PageToolbarButton>

      <PageToolbarButton
        label="Advanced"
        iconColor={showAdvanced ? "var(--advancedFormLabelColor)" : undefined}
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

      {#if subTab === "general"}
        <!-- Host -->
        <FieldSet legend="Host">
          <FormGroup
            label="Port Number"
            helpText="Port the internal HTTP web server listens on"
            isDirty={isFieldDirty("port")}
          >
            <input
              type="number"
              class="formInput"
              bind:value={formConfig.port}
            />
          </FormGroup>

          <FormGroup
            label="URL Base"
            helpText="For reverse proxy subpath support (e.g. /anilist-sync)"
            isDirty={isFieldDirty("urlBase")}
          >
            <input
              type="text"
              class="formInput"
              bind:value={formConfig.urlBase}
            />
          </FormGroup>

          {#if showAdvanced}
            <FormGroup
              label="Allowed Hosts"
              helpText="Allowed hostnames/domains or '*' for all (comma-separated). Localhost and IP addresses are always allowed."
              isAdvanced={true}
              isDirty={isFieldDirty("allowedHosts")}
            >
              <input
                type="text"
                class="formInput"
                placeholder="e.g. sync.example.com or *"
                bind:value={formConfig.allowedHosts}
              />
            </FormGroup>

            <FormGroup
              label="Bind Address"
              helpText="Network interface address the HTTP listener binds to (* or 0.0.0.0 for all interfaces)"
              isAdvanced={true}
              isDirty={isFieldDirty("bindAddress")}
            >
              <input
                type="text"
                class="formInput"
                bind:value={formConfig.bindAddress}
              />
            </FormGroup>

            <FormGroup
              label="Application URL"
              helpText="URL used to access this application externally, used for notifications and redirect URLs"
              isAdvanced={true}
              isDirty={isFieldDirty("applicationUrl")}
            >
              <input
                type="text"
                class="formInput"
                placeholder="http://localhost:7171"
                bind:value={formConfig.applicationUrl}
              />
            </FormGroup>

            <FormGroup
              label="Instance Name"
              helpText="Display name rendered in header and page titles"
              isAdvanced={true}
              isDirty={isFieldDirty("instanceName")}
            >
              <input
                type="text"
                class="formInput"
                bind:value={formConfig.instanceName}
              />
            </FormGroup>

            <FormGroup
              label="Enable SSL"
              helpText="Enables HTTPS listener on configured SSL port with certificates"
              isAdvanced={true}
              isDirty={isFieldDirty("enableSsl")}
            >
              <CheckInput
                checked={!!formConfig.enableSsl}
                on:change={(e) => (formConfig.enableSsl = e.detail)}
              />
            </FormGroup>

            {#if formConfig.enableSsl}
              <FormGroup
                label="SSL Port"
                helpText="Port the internal HTTPS listener listens on"
                isAdvanced={true}
                isDirty={isFieldDirty("sslPort")}
              >
                <input
                  type="number"
                  class="formInput"
                  bind:value={formConfig.sslPort}
                />
              </FormGroup>
            {/if}
          {/if}
        </FieldSet>

        <!-- Security -->
        <FieldSet legend="Security">
          <FormGroup
            label="Authentication Method"
            helpText="Authentication mode: Basic (Browser popup) or Forms (Login Page)"
            isDirty={isFieldDirty("authenticationMethod")}
          >
            <select
              class="formInput"
              bind:value={formConfig.authenticationMethod}
            >
              <option value="Basic">Basic (Browser popup)</option>
              <option value="Forms">Forms (Login Page)</option>
            </select>
          </FormGroup>

          <FormGroup
            label="Authentication Required"
            helpText="Controls whether authentication applies to all clients or is disabled for local LAN addresses"
            isDirty={isFieldDirty("authenticationRequired")}
          >
            <select
              class="formInput"
              bind:value={formConfig.authenticationRequired}
            >
              <option value="Enabled">Enabled (All Clients)</option>
              <option value="DisabledForLocalAddresses"
                >Disabled for Local Addresses</option
              >
            </select>
          </FormGroup>

          <FormGroup
            label="Trusted Networks"
            helpText="Comma-separated IP addresses or CIDR blocks allowed to proxy client addresses via X-Forwarded-For"
            isDirty={isFieldDirty("trustedNetworks")}
          >
            <input
              type="text"
              class="formInput"
              placeholder="127.0.0.1/32,::1/128,172.16.0.0/12,192.168.0.0/16,10.0.0.0/8"
              bind:value={formConfig.trustedNetworks}
            />
          </FormGroup>

          <FormGroup
            label="Username"
            helpText="Username for basic and forms authentication"
            isDirty={isFieldDirty("username")}
          >
            <input
              type="text"
              class="formInput"
              bind:value={formConfig.username}
            />
          </FormGroup>

          <FormGroup
            label="Password"
            helpText="Leave blank to keep existing password"
          >
            <input
              type="password"
              class="formInput"
              placeholder="New password"
              bind:value={formPassword}
            />
          </FormGroup>

          {#if formPassword}
            <FormGroup
              label="Password Confirmation"
              helpText="Confirm the password entered above"
            >
              <input
                type="password"
                class="formInput"
                placeholder="Confirm new password"
                bind:value={formPasswordConfirm}
              />
            </FormGroup>
          {/if}

          <FormGroup
            label="API Key"
            helpText="Secret key used for API authentication and state-mutating requests"
            isDirty={isFieldDirty("apiKey")}
          >
            <input
              type="text"
              class="formInput hasButton"
              bind:value={formConfig.apiKey}
              readonly
            />
            <button
              type="button"
              class="formInputButton"
              on:click={copyApiKey}
              title="Copy API Key"
            >
              <Copy size={16} />
            </button>
            <button
              type="button"
              class="formInputButton dangerButton"
              on:click={resetApiKey}
              title="Reset API Key"
            >
              <RefreshCw size={16} />
            </button>
          </FormGroup>
        </FieldSet>

        <!-- Proxy -->
        <FieldSet legend="Proxy">
          <FormGroup
            label="Use Proxy"
            helpText="Route outgoing HTTP and API requests through a proxy server"
            isDirty={isFieldDirty("useProxy")}
          >
            <CheckInput
              checked={!!formConfig.useProxy}
              on:change={(e) => (formConfig.useProxy = e.detail)}
            />
          </FormGroup>

          {#if formConfig.useProxy}
            <FormGroup label="Proxy Type" isDirty={isFieldDirty("proxyType")}>
              <select class="formInput" bind:value={formConfig.proxyType}>
                <option value="Http">HTTP(S)</option>
                <option value="Socks4">SOCKS4</option>
                <option value="Socks5">SOCKS5</option>
              </select>
            </FormGroup>

            <FormGroup label="Hostname" isDirty={isFieldDirty("proxyHostname")}>
              <input
                type="text"
                class="formInput"
                placeholder="e.g. proxy.example.com"
                bind:value={formConfig.proxyHostname}
              />
            </FormGroup>

            <FormGroup label="Port" isDirty={isFieldDirty("proxyPort")}>
              <input
                type="number"
                class="formInput"
                placeholder="8080"
                bind:value={formConfig.proxyPort}
              />
            </FormGroup>

            <FormGroup
              label="Username"
              helpText="You only need to enter a username and password if one is required. Leave them blank otherwise."
              isDirty={isFieldDirty("proxyUsername")}
            >
              <input
                type="text"
                class="formInput"
                bind:value={formConfig.proxyUsername}
              />
            </FormGroup>

            <FormGroup
              label="Password"
              helpText="You only need to enter a username and password if one is required. Leave them blank otherwise."
              isDirty={isFieldDirty("proxyPassword")}
            >
              <input
                type="password"
                class="formInput"
                bind:value={formConfig.proxyPassword}
              />
            </FormGroup>

            <FormGroup
              label="Ignored Addresses"
              helpText="Use ',' as a separator, and '*.' as a wildcard for subdomains"
              isDirty={isFieldDirty("proxyIgnoredAddresses")}
            >
              <input
                type="text"
                class="formInput"
                placeholder="localhost, 127.0.0.1"
                bind:value={formConfig.proxyIgnoredAddresses}
              />
            </FormGroup>

            <FormGroup
              label="Bypass Proxy for Local Addresses"
              isDirty={isFieldDirty("proxyBypassLocalAddresses")}
            >
              <CheckInput
                checked={formConfig.proxyBypassLocalAddresses ?? true}
                on:change={(e) =>
                  (formConfig.proxyBypassLocalAddresses = e.detail)}
              />
            </FormGroup>
          {/if}
        </FieldSet>

        <!-- Logging -->
        <FieldSet legend="Logging">
          <FormGroup
            label="Log Level"
            helpText="Minimum severity level recorded to disk logs"
            isDirty={isFieldDirty("logLevel")}
          >
            <select class="formInput" bind:value={formConfig.logLevel}>
              <option value="Trace">Trace</option>
              <option value="Debug">Debug</option>
              <option value="Info">Info</option>
              <option value="Warn">Warn</option>
              <option value="Error">Error</option>
            </select>
          </FormGroup>

          {#if showAdvanced}
            <FormGroup
              label="Log Size Limit"
              helpText="Maximum log file size in MB before archiving. Default is 1MB."
              isAdvanced={true}
              isDirty={isFieldDirty("logSizeLimit")}
            >
              <input
                type="number"
                min="1"
                class="formInput hasAddon"
                bind:value={formConfig.logSizeLimit}
              />
              <span class="formInputAddon">MB</span>
            </FormGroup>
          {/if}
        </FieldSet>

        {#if showAdvanced}
          <!-- Updates -->
          <FieldSet legend="Updates">
            <FormGroup
              label="Branch"
              helpText="Branch used by external update mechanism"
              isAdvanced={true}
              isDirty={isFieldDirty("updateBranch")}
            >
              <input
                type="text"
                class="formInput"
                placeholder="main"
                bind:value={formConfig.updateBranch}
              />
            </FormGroup>

            <FormGroup
              label="Automatic"
              helpText="Automatically download and install updates. You will still be able to install from System: Updates"
              warningText={formConfig.updateMechanism === "Docker"
                ? "Automatic updates are disabled when using Docker. Container images must be updated externally via Docker tags."
                : formConfig.updateMechanism === "Package"
                  ? "Automatic updates are disabled when using Package. Updates must be applied externally via your package manager."
                  : ""}
              isAdvanced={true}
              isDirty={isFieldDirty("updateAutomatically")}
            >
              <CheckInput
                checked={!!formConfig.updateAutomatically}
                disabled={formConfig.updateMechanism !== "BuiltIn"}
                on:change={(e) => (formConfig.updateAutomatically = e.detail)}
              />
            </FormGroup>

            <FormGroup
              label="Mechanism"
              helpText="Method used to apply updates (Built-in, Docker, or Package)."
              isAdvanced={true}
              isDirty={isFieldDirty("updateMechanism")}
            >
              <select
                class="formInput"
                bind:value={formConfig.updateMechanism}
                on:change={() => {
                  if (formConfig.updateMechanism !== "BuiltIn") {
                    formConfig.updateAutomatically = false;
                  }
                }}
              >
                <option value="BuiltIn">Built-in</option>
                <option value="Docker">Docker</option>
                <option value="Package">Package</option>
              </select>
            </FormGroup>
          </FieldSet>

          <!-- Backups -->
          <FieldSet legend="Backups">
            <FormGroup
              label="Folder"
              helpText="Relative paths will be under the application data directory"
              isAdvanced={true}
              isDirty={isFieldDirty("backupFolder")}
            >
              <input
                type="text"
                class="formInput hasButton"
                placeholder="Backups"
                bind:value={formConfig.backupFolder}
              />
              <button
                type="button"
                class="formInputButton primaryButton"
                title="Browse Folder"
                on:click={() => (isFolderBrowserOpen = true)}
              >
                <Folder size={16} />
              </button>
            </FormGroup>

            <FormGroup
              label="Interval"
              helpText="Interval between automatic backups"
              isAdvanced={true}
              isDirty={isFieldDirty("backupInterval")}
            >
              <input
                type="number"
                min="1"
                class="formInput hasAddon"
                placeholder="7"
                bind:value={formConfig.backupInterval}
              />
              <span class="formInputAddon">days</span>
            </FormGroup>

            <FormGroup
              label="Retention"
              helpText="Automatic backups older than the retention period will be cleaned up automatically"
              isAdvanced={true}
              isDirty={isFieldDirty("backupRetention")}
            >
              <input
                type="number"
                min="1"
                class="formInput hasAddon"
                placeholder="28"
                bind:value={formConfig.backupRetention}
              />
              <span class="formInputAddon">days</span>
            </FormGroup>
          </FieldSet>
        {/if}
      {:else if subTab === "sync"}
        <!-- AniList & Sync Settings -->
        <FieldSet legend="AniList Configuration">
          <FormGroup
            label="AniList Username"
            helpText="The AniList user account to sync anime library lists from"
            isDirty={isFieldDirty("aniListUsername")}
          >
            <input
              type="text"
              class="formInput"
              bind:value={formConfig.aniListUsername}
            />
          </FormGroup>

          <FormGroup
            label="Sync Interval (Hours)"
            helpText="Interval in hours between automated background synchronization cycles"
            isDirty={isFieldDirty("syncIntervalHours")}
          >
            <input
              type="number"
              class="formInput"
              min="1"
              max="168"
              bind:value={formConfig.syncIntervalHours}
            />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Matching Rules & Policy">
          {#if showAdvanced}
            <FormGroup
              label="Date Tolerance (Days)"
              helpText="Maximum allowable divergence in release dates when matching entries"
              isAdvanced={true}
              isDirty={isFieldDirty("dateToleranceDays")}
            >
              <input
                type="number"
                class="formInput"
                min="0"
                max="365"
                bind:value={formConfig.dateToleranceDays}
              />
            </FormGroup>

            <FormGroup
              label="Confidence Threshold"
              helpText="Minimum fuzzy match score (0.0 - 1.0) required to auto-bind without review diversion"
              isAdvanced={true}
              isDirty={isFieldDirty("confidenceThreshold")}
            >
              <input
                type="number"
                step="0.05"
                min="0"
                max="1"
                class="formInput"
                bind:value={formConfig.confidenceThreshold}
              />
            </FormGroup>
          {/if}

          <FormGroup
            label="Unmonitor Dropped"
            helpText="Automatically unmonitor media in Sonarr/Radarr when dropped on AniList"
            isDirty={isFieldDirty("unmonitorDropped")}
          >
            <CheckInput
              checked={!!formConfig.unmonitorDropped}
              on:change={(e) => (formConfig.unmonitorDropped = e.detail)}
            />
          </FormGroup>

          <FormGroup
            label="First Run Dry Run"
            helpText="Stage all actions for approval on the first sync cycle instead of applying immediately"
            isDirty={isFieldDirty("firstRunDryRun")}
          >
            <CheckInput
              checked={!!formConfig.firstRunDryRun}
              on:change={(e) => (formConfig.firstRunDryRun = e.detail)}
            />
          </FormGroup>
        </FieldSet>
      {:else if subTab === "servarr"}
        <!-- Servarr -->
        <FieldSet legend="Sonarr">
          <FormGroup
            label="Sonarr URL"
            helpText="Full base URL to your Sonarr v3/v4 server (e.g. http://localhost:8989)"
            isDirty={isFieldDirty("sonarrUrl")}
          >
            <input
              type="text"
              class="formInput"
              placeholder="http://localhost:8989"
              bind:value={formConfig.sonarrUrl}
            />
          </FormGroup>

          <FormGroup
            label="Sonarr API Key"
            helpText="API key from Sonarr -> Settings -> General -> Security"
            isDirty={isFieldDirty("sonarrApiKey")}
          >
            <input
              type="text"
              class="formInput"
              bind:value={formConfig.sonarrApiKey}
            />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Radarr">
          <FormGroup
            label="Radarr URL"
            helpText="Full base URL to your Radarr v3/v4/v5 server (e.g. http://localhost:7878)"
            isDirty={isFieldDirty("radarrUrl")}
          >
            <input
              type="text"
              class="formInput"
              placeholder="http://localhost:7878"
              bind:value={formConfig.radarrUrl}
            />
          </FormGroup>

          <FormGroup
            label="Radarr API Key"
            helpText="API key from Radarr -> Settings -> General -> Security"
            isDirty={isFieldDirty("radarrApiKey")}
          >
            <input
              type="text"
              class="formInput"
              bind:value={formConfig.radarrApiKey}
            />
          </FormGroup>
        </FieldSet>

        <FieldSet legend="Tagging">
          <FormGroup
            label="Arr Tag Name"
            helpText="Tag applied to items managed by AniList sync in Sonarr and Radarr"
            isDirty={isFieldDirty("tagName")}
          >
            <input
              type="text"
              class="formInput"
              placeholder="anilist"
              bind:value={formConfig.tagName}
            />
          </FormGroup>
        </FieldSet>
      {:else if subTab === "connect"}
        <!-- Connect Notifications -->
        <FieldSet legend="Notification Connections">
          <div class="notificationsGrid">
            {#each notifications as notif (notif.id)}
              <div
                class="notificationCard"
                on:click={() => openEditNotification(notif)}
                role="button"
                tabindex="0"
                on:keydown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    openEditNotification(notif);
                  }
                }}
              >
                <div class="notifHeader">
                  <span class="notifName">{notif.name}</span>
                  <Label kind="primary" size="small"
                    >{notif.type || notif.provider}</Label
                  >
                </div>
                <div class="notifUrl">
                  {notif.settings?.webhookUrl ||
                    notif.settings?.url ||
                    "Webhook URL configured"}
                </div>
                <div class="notifTriggers">
                  {#if notif.onAdded || notif.onSyncStart}<Label
                      kind="default"
                      size="small">Added</Label
                    >{/if}
                  {#if notif.onReview || notif.onReviewQueueNeeded}<Label
                      kind="warning"
                      size="small">Review Needed</Label
                    >{/if}
                  {#if notif.onComplete || notif.onSyncSuccess}<Label
                      kind="success"
                      size="small">Complete</Label
                    >{/if}
                  {#if notif.onError || notif.onSyncFailure}<Label
                      kind="danger"
                      size="small">Error</Label
                    >{/if}
                  {#if notif.onUpdate}<Label kind="info" size="small"
                      >Update</Label
                    >{/if}
                </div>
                <div class="notifActions">
                  <button
                    class="notifActionBtn"
                    on:click|stopPropagation={() => openEditNotification(notif)}
                    title="Edit"
                  >
                    <Edit size={16} />
                  </button>
                  <button
                    class="notifActionBtn deleteBtn"
                    on:click|stopPropagation={() =>
                      deleteNotification(notif.id)}
                    title="Delete"
                  >
                    <Trash2 size={16} />
                  </button>
                </div>
              </div>
            {/each}

            <!-- 290px Add Notification Card -->
            <button
              type="button"
              class="addNotificationCard"
              on:click={openAddNotification}
            >
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
        <Button
          kind="default"
          size="medium"
          on:click={resetConfig}
          disabled={saving}
        >
          Reset
        </Button>
        <Button
          kind="primary"
          size="medium"
          on:click={saveConfig}
          disabled={saving}
        >
          <Save size={16} class={saving ? "spinning" : ""} /> Save Changes
        </Button>
      </div>
    </PageContentFooter>
  {/if}

  <!-- Add / Edit Notification Connection Modal -->
  <Modal
    isOpen={isNotifModalOpen}
    title={notifModalTitle}
    size="medium"
    on:close={() => (isNotifModalOpen = false)}
  >
    <div slot="body">
      {#if notifModalStep === "select"}
        <div class="notifSelectSubtitle">
          Select a notification connection to add:
        </div>

        <div class="connectionSelectorGrid">
          <button
            type="button"
            class="connectionSelectCard"
            on:click={() => selectConnectionType("DISCORD")}
          >
            <div class="connectionSelectIcon discordIcon">
              <svg
                width="38"
                height="38"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path
                  d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028c.462-.63.874-1.295 1.226-1.994.021-.041.001-.09-.041-.106a13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.929 1.793 8.18 1.793 12.061 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.894.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.028zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z"
                />
              </svg>
            </div>
            <div class="connectionSelectName">Discord</div>
            <div class="connectionSelectDescription">
              Send notifications to a Discord channel via webhook with rich
              embedded message cards.
            </div>
          </button>

          <button
            type="button"
            class="connectionSelectCard"
            on:click={() => selectConnectionType("WEBHOOK")}
          >
            <div class="connectionSelectIcon webhookIcon">
              <Webhook size={38} />
            </div>
            <div class="connectionSelectName">Webhook</div>
            <div class="connectionSelectDescription">
              Send HTTP POST requests with structured JSON event payloads to any
              custom webhook endpoint.
            </div>
          </button>
        </div>
      {:else}
        {#if notifError}
          <Alert kind="danger">{notifError}</Alert>
        {/if}
        {#if notifTestMsg}
          <Alert kind="success">{notifTestMsg}</Alert>
        {/if}

        <FormGroup
          label="Name"
          helpText="Display name for this notification connection"
        >
          <input
            type="text"
            class="formInput"
            placeholder={notifType === "DISCORD" ? "Discord" : "Webhook"}
            bind:value={notifName}
          />
        </FormGroup>

        <FieldSet legend="Notification Triggers">
          <FormGroup
            label="On Media Added"
            helpText="Notify when anime series or movies are added or monitored"
          >
            <CheckInput
              checked={notifOnAdded}
              on:change={(e) => (notifOnAdded = e.detail)}
            />
          </FormGroup>

          <FormGroup
            label="On Review Needed"
            helpText="Notify when titles require manual matching review in the queue"
          >
            <CheckInput
              checked={notifOnReview}
              on:change={(e) => (notifOnReview = e.detail)}
            />
          </FormGroup>

          <FormGroup
            label="On Sync Complete"
            helpText="Notify when a synchronization cycle completes successfully"
          >
            <CheckInput
              checked={notifOnComplete}
              on:change={(e) => (notifOnComplete = e.detail)}
            />
          </FormGroup>

          <FormGroup
            label="On Sync Error"
            helpText="Notify when synchronization fails or encounters errors"
          >
            <CheckInput
              checked={notifOnError}
              on:change={(e) => (notifOnError = e.detail)}
            />
          </FormGroup>

          <FormGroup
            label="On Update Available"
            helpText="Notify when a new version of AniList Arr Sync is available"
          >
            <CheckInput
              checked={notifOnUpdate}
              on:change={(e) => (notifOnUpdate = e.detail)}
            />
          </FormGroup>
        </FieldSet>

        <FieldSet
          legend={notifType === "DISCORD"
            ? "Discord Settings"
            : "Webhook Settings"}
        >
          {#if notifType === "DISCORD"}
            <FormGroup
              label="Webhook URL"
              helpText="Discord webhook URL configured in channel settings under Integrations -> Webhooks"
            >
              <input
                type="text"
                class="formInput"
                placeholder="https://discord.com/api/webhooks/..."
                bind:value={notifWebhookUrl}
              />
            </FormGroup>
          {:else}
            <FormGroup
              label="Webhook URL"
              helpText="HTTP URL to deliver JSON notification payloads to"
            >
              <input
                type="text"
                class="formInput"
                placeholder="https://example.com/webhook"
                bind:value={notifWebhookUrl}
              />
            </FormGroup>

            <FormGroup
              label="Method"
              helpText="HTTP method used for generic webhook requests"
            >
              <input
                type="text"
                class="formInput"
                value="POST"
                readonly
                disabled
              />
            </FormGroup>
          {/if}
        </FieldSet>
      {/if}
    </div>

    <div
      slot="footer"
      class="modalFooterContainer {notifModalStep === 'select'
        ? 'alignRight'
        : ''}"
    >
      {#if notifModalStep === "select"}
        <Button kind="default" on:click={() => (isNotifModalOpen = false)}>
          Cancel
        </Button>
      {:else}
        <div class="footerLeft">
          <Button
            kind="default"
            on:click={testNotification}
            disabled={notifTesting}
          >
            <Send size={14} class={notifTesting ? "spinning" : ""} /> Test
          </Button>
        </div>
        <div class="footerRight">
          {#if !editingNotif}
            <Button
              kind="default"
              on:click={() => (notifModalStep = "select")}
            >
              <ArrowLeft size={14} /> Back
            </Button>
          {:else}
            <Button
              kind="danger"
              on:click={() => {
                const id = editingNotif?.id;
                if (id) {
                  deleteNotification(id);
                  isNotifModalOpen = false;
                }
              }}
            >
              <Trash2 size={14} /> Delete
            </Button>
          {/if}
          <Button kind="default" on:click={() => (isNotifModalOpen = false)}>
            Cancel
          </Button>
          <Button kind="primary" on:click={saveNotification} disabled={notifSaving}>
            <Save size={14} /> {notifSaving ? 'Saving...' : 'Save Connection'}
          </Button>
        </div>
      {/if}
    </div>
  </Modal>

  <FolderBrowserModal
    isOpen={isFolderBrowserOpen}
    initialPath={formConfig.backupFolder || ''}
    on:close={() => (isFolderBrowserOpen = false)}
    on:select={(e) => {
      formConfig.backupFolder = e.detail;
      formConfig = { ...formConfig };
    }}
  />
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
    cursor: pointer;
    transition:
      border-color 150ms ease,
      background-color 150ms ease,
      transform 150ms ease;
  }

  .notificationCard:hover {
    border-color: var(--sonarrBlue);
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
    transform: translateY(-2px);
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
    transition:
      background-color 200ms ease,
      border-color 200ms ease;
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

  /* Connection Selector Modal */
  .notifSelectSubtitle {
    color: var(--helpTextColor);
    font-size: var(--defaultFontSize);
    margin-bottom: 16px;
  }

  .connectionSelectorGrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 16px;
    padding: 4px 0 16px;
  }

  .connectionSelectCard {
    background-color: var(--cardBackgroundColor);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px;
    padding: 24px 20px;
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    cursor: pointer;
    transition:
      border-color 180ms ease,
      background-color 180ms ease,
      transform 180ms ease,
      box-shadow 180ms ease;
    outline: none;
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2);
  }

  .connectionSelectCard:hover {
    border-color: var(--sonarrBlue);
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
    transform: translateY(-2px);
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.4);
  }

  .connectionSelectIcon {
    width: 48px;
    height: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-bottom: 14px;
  }

  .discordIcon {
    color: #5865f2;
  }

  .webhookIcon {
    color: var(--sonarrBlue);
  }

  .connectionSelectName {
    font-size: 16px;
    font-weight: 600;
    color: var(--white);
    margin-bottom: 6px;
  }

  .connectionSelectDescription {
    font-size: 13px;
    color: var(--helpTextColor);
    line-height: 1.45;
  }

  .modalFooterContainer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    width: 100%;
  }

  .alignRight {
    justify-content: flex-end;
  }

  .footerLeft,
  .footerRight {
    display: flex;
    align-items: center;
    gap: 8px;
  }
</style>
