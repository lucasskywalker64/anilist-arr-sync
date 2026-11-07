<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { api, getBaseUrl, waitForRestart } from '../../lib/api';
  import type { AniListProfile, AppConfig, ServarrConnectionOptions } from '../../lib/types';
  import Alert from '../common/Alert.svelte';
  import Button from '../common/Button.svelte';
  import ServarrOnboardingStep from './ServarrOnboardingStep.svelte';

  export let config: AppConfig;
  export let activated = false;

  const dispatch = createEventDispatcher<{ dismiss: void; finish: void }>();
  const steps = ['Welcome', 'AniList', 'Sonarr', 'Radarr', 'Sync Preferences', 'Summary'];
  const polling = new AbortController();
  onDestroy(() => polling.abort());

  function connection(url: string, key: string, profileId: number, rootFolder: string) {
    return { baseUrl: url, apiKey: '', savedUrl: url, hasSavedKey: Boolean(key), profileId, rootFolder,
      options: null as ServarrConnectionOptions | null };
  }

  let step = 0;
  let username = config.aniListUsername;
  let profile: AniListProfile | null = null;
  let sonarr = connection(config.sonarrUrl, config.sonarrApiKey, config.sonarrQualityProfileId, config.sonarrRootFolderPath);
  let radarr = connection(config.radarrUrl, config.radarrApiKey, config.radarrQualityProfileId, config.radarrRootFolderPath);
  let monitorOnAdd = config.sonarrMonitorOnAdd;
  let enableRadarr = Boolean(config.radarrUrl);
  let dryRun = config.firstRunDryRun;
  let interval = config.syncIntervalHours;
  $: intervals = [...new Set([1, 2, 4, 8, 12, 24, interval])].sort((a, b) => a - b);
  let busy = false;
  let error = '';
  let saved = activated;
  let restartPending = false;
  let reconnectUrl = '';

  $: activeStep = steps[step];
  $: canAdvance = activeStep === 'AniList' ? Boolean(profile)
    : activeStep === 'Sonarr' ? Boolean(sonarr.options)
    : activeStep === 'Radarr' ? !enableRadarr || Boolean(radarr.options)
    : true;

  function move(offset: number) {
    error = '';
    step += offset;
  }

  async function verifyAccount() {
    busy = true;
    profile = null;
    error = '';
    try {
      profile = await api.testAniList(username.trim());
    } catch (err) {
      error = err instanceof Error ? err.message : 'Could not verify AniList account.';
    } finally {
      busy = false;
    }
  }

  async function activate() {
    const result = await api.restart();
    const target = new URL(result.reconnectUrl);
    const needsNavigation = target.origin !== window.location.origin || target.pathname !== `${getBaseUrl()}/`;
    if (needsNavigation) target.searchParams.set('onboarding', 'complete');
    reconnectUrl = target.href;
    await waitForRestart(result.instanceId, result.reconnectUrl, polling.signal, result.statusUrl);
    if (needsNavigation) {
      window.location.assign(reconnectUrl);
      return;
    }
    reconnectUrl = '';
    restartPending = false;
  }

  async function finishSetup() {
    busy = true;
    error = '';
    try {
      if (!saved) {
        const result = await api.updateConfig({
          wizardCompleted: true,
          aniListUsername: profile?.username ?? username.trim(),
          sonarrUrl: sonarr.baseUrl.trim(),
          ...(sonarr.apiKey.trim() ? { sonarrApiKey: sonarr.apiKey.trim() } : {}),
          sonarrQualityProfileId: sonarr.profileId,
          sonarrRootFolderPath: sonarr.rootFolder,
          sonarrMonitorOnAdd: monitorOnAdd,
          ...(enableRadarr ? {
            radarrUrl: radarr.baseUrl.trim(),
            ...(radarr.apiKey.trim() ? { radarrApiKey: radarr.apiKey.trim() } : {}),
            radarrQualityProfileId: radarr.profileId,
            radarrRootFolderPath: radarr.rootFolder,
          } : { radarrUrl: '', radarrApiKey: '' }),
          firstRunDryRun: dryRun,
          syncIntervalHours: interval,
        });
        saved = true;
        restartPending = Boolean(result.restartRequired);
      }
      if (restartPending) await activate();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Could not finish setup.';
    } finally {
      busy = false;
    }
  }

  async function startDryRun() {
    busy = true;
    error = '';
    try {
      await api.startSync(true);
      dispatch('finish');
    } catch (err) {
      error = err instanceof Error ? err.message : 'Could not start the initial dry run.';
    } finally {
      busy = false;
    }
  }
</script>

<main class="onboarding">
  <section aria-labelledby="wizard-heading">
    {#if !saved}<p class="step">Setup step {step + 1} of {steps.length}</p>{/if}
    <h1 id="wizard-heading">{saved ? 'Setup Saved' : activeStep}</h1>
    {#if error}<Alert kind="danger">{error}</Alert>{/if}

    {#if saved}
      {#if restartPending}
        <p role="status">{busy ? 'Restarting to activate your settings...' : 'Restart the application to activate your settings.'}</p>
        {#if reconnectUrl && !busy}<a href={reconnectUrl}>Reconnect to application</a>{/if}
      {:else}
        <p>Your settings are active. Run a preview now to review the proposed changes before applying them.</p>
      {/if}
    {:else if activeStep === 'Welcome'}
      <p>AniList Arr Sync keeps Sonarr and Radarr in sync with your AniList watchlist.</p>
      <p>Have your AniList username and your Sonarr URL and API key ready. You can also connect Radarr to sync movies.</p>
    {:else if activeStep === 'AniList'}
      <label for="onboarding-anilist">AniList Username</label>
      <input id="onboarding-anilist" bind:value={username} disabled={busy} on:input={() => { profile = null; error = ''; }} />
      <Button on:click={verifyAccount} disabled={busy || !username.trim()}>{busy ? 'Verifying...' : 'Verify Account'}</Button>
      {#if profile}
        <p class="profile" role="status">
          {#if profile.avatarUrl}<img src={profile.avatarUrl} alt="" width="40" height="40" />{/if}
          {profile.username} · {profile.animeCount} anime
        </p>
      {/if}
    {:else if activeStep === 'Sonarr'}
      <ServarrOnboardingStep service="sonarr" bind:draft={sonarr} bind:busy />
      <label class="checkbox"><input type="checkbox" bind:checked={monitorOnAdd} /> Monitor anime series on add</label>
    {:else if activeStep === 'Radarr'}
      <label class="checkbox"><input type="checkbox" bind:checked={enableRadarr} disabled={busy} /> Enable movie synchronization</label>
      {#if enableRadarr}
        <ServarrOnboardingStep service="radarr" bind:draft={radarr} bind:busy />
      {:else}
        <p>Continue to set up series synchronization only.</p>
      {/if}
    {:else if activeStep === 'Sync Preferences'}
      <label class="checkbox"><input type="checkbox" bind:checked={dryRun} /> Dry run mode</label>
      <p>Stage proposed changes for review before applying them to Sonarr or Radarr.</p>
      <label for="onboarding-interval">Sync Interval</label>
      <select id="onboarding-interval" bind:value={interval}>
        {#each intervals as hours}<option value={hours}>{hours} {hours === 1 ? 'hour' : 'hours'}</option>{/each}
      </select>
    {:else}
      <dl>
        <dt>AniList</dt><dd>{profile?.username}</dd>
        <dt>Sonarr</dt><dd>{sonarr.baseUrl} · {sonarr.options?.qualityProfiles.find(p => p.id === sonarr.profileId)?.name} · {sonarr.rootFolder}</dd>
        <dt>Monitor series on add</dt><dd>{monitorOnAdd ? 'On' : 'Off'}</dd>
        <dt>Radarr</dt><dd>{enableRadarr ? `${radarr.baseUrl} · ${radarr.options?.qualityProfiles.find(p => p.id === radarr.profileId)?.name} · ${radarr.rootFolder}` : 'Skipped'}</dd>
        <dt>Sync</dt><dd>Every {interval} h · Dry run {dryRun ? 'on' : 'off'}</dd>
      </dl>
      <p>Saving changes to service connections restarts the application.</p>
    {/if}

    <footer>
      {#if saved}
        {#if restartPending && !busy}<Button on:click={finishSetup}>Retry Restart</Button>{/if}
        <Button disabled={busy} on:click={() => dispatch('finish')}>Go to Dashboard</Button>
        <Button kind="primary" disabled={busy || restartPending} on:click={startDryRun}>Start Initial Dry Run</Button>
      {:else}
        <Button disabled={busy} on:click={() => dispatch('dismiss')}>Configure Later</Button>
        {#if step > 0}<Button disabled={busy} on:click={() => move(-1)}>Back</Button>{/if}
        {#if step < steps.length - 1}
          <Button kind="primary" disabled={busy || !canAdvance} on:click={() => move(1)}>Next</Button>
        {:else}
          <Button kind="primary" disabled={busy} on:click={finishSetup}>{busy ? 'Saving...' : 'Finish Setup'}</Button>
        {/if}
      {/if}
    </footer>
  </section>
</main>

<style>
  .onboarding { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
  section { width: min(100%, 600px); padding: clamp(20px, 5vw, 36px); background: var(--pageHeaderBackgroundColor); border: 1px solid var(--defaultBorderColor); border-radius: var(--borderRadius); }
  h1 { margin: 0 0 20px; color: var(--white); }
  .step { margin: 0 0 8px; color: var(--themeBlue); }
  section :global(label:not(.checkbox)) { display: block; margin: 16px 0 6px; }
  section :global(input:not([type='checkbox'])), section :global(select) { display: block; width: 100%; margin-bottom: 12px; padding: 8px; color: var(--textColor); background: var(--inputBackgroundColor); border: 1px solid var(--defaultBorderColor); border-radius: var(--borderRadius); }
  .checkbox { display: flex; align-items: center; gap: 8px; margin: 16px 0; }
  .profile { display: flex; align-items: center; gap: 10px; }
  .profile img { border-radius: 50%; object-fit: cover; }
  dl { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; }
  dt { font-weight: 600; }
  dd { margin: 0; overflow-wrap: anywhere; }
  footer { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; margin-top: 24px; }
</style>
