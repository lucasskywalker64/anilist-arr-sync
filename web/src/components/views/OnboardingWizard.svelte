<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { api, getBaseUrl, waitForRestart } from '../../lib/api';
  import type { AniListProfile, AppConfig, ServarrConnectionOptions } from '../../lib/types';
  import Alert from '../common/Alert.svelte';
  import Button from '../common/Button.svelte';
  import ServarrOnboardingStep from './ServarrOnboardingStep.svelte';
  import AniListStatusPicker, { describeListStatuses } from './AniListStatusPicker.svelte';

  export let config: AppConfig;
  export let resume = false;

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
  let seriesStatuses = config.aniListTvStatus;
  let movieStatuses = config.aniListMovieStatus;
  let profile: AniListProfile | null = null;
  let sonarr = connection(config.sonarrUrl, config.sonarrApiKey, config.sonarrQualityProfileId, config.sonarrRootFolderPath);
  let radarr = connection(config.radarrUrl, config.radarrApiKey, config.radarrQualityProfileId, config.radarrRootFolderPath);
  let sonarrMonitorOnAdd = config.sonarrMonitorOnAdd;
  let sonarrSearchOnAdd = config.sonarrSearchOnAdd ?? false;
  let radarrMonitorOnAdd = config.radarrMonitorOnAdd ?? true;
  let radarrSearchOnAdd = config.radarrSearchOnAdd ?? false;
  let enableRadarr = Boolean(config.radarrUrl);
  let dryRun = config.firstRunDryRun;
  let interval: number | undefined = config.syncIntervalHours ?? 4;
  let busy = false;
  let error = '';
  let saved = resume;
  let restartPending = resume && Boolean(config.restartRequired);
  let reconnectUrl = '';

  $: activeStep = steps[step];
  $: validInterval = typeof interval === 'number' && Number.isSafeInteger(interval) && interval > 0;
  $: canAdvance = activeStep === 'AniList' ? Boolean(profile)
    : activeStep === 'Sonarr' ? Boolean(sonarr.options)
    : activeStep === 'Radarr' ? !enableRadarr || Boolean(radarr.options)
    : activeStep === 'Sync Preferences' ? validInterval
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
          aniListTvStatus: seriesStatuses,
          sonarrUrl: sonarr.baseUrl.trim(),
          ...(sonarr.apiKey.trim() ? { sonarrApiKey: sonarr.apiKey.trim() } : {}),
          sonarrQualityProfileId: sonarr.profileId,
          sonarrRootFolderPath: sonarr.rootFolder,
          sonarrMonitorOnAdd,
          sonarrSearchOnAdd,
          ...(enableRadarr ? {
            radarrUrl: radarr.baseUrl.trim(),
            ...(radarr.apiKey.trim() ? { radarrApiKey: radarr.apiKey.trim() } : {}),
            radarrQualityProfileId: radarr.profileId,
            radarrRootFolderPath: radarr.rootFolder,
            radarrMonitorOnAdd,
            radarrSearchOnAdd,
            aniListMovieStatus: movieStatuses,
          } : { radarrUrl: '', radarrApiKey: '' }),
          firstRunDryRun: dryRun,
          syncIntervalHours: interval,
        });
        const url = new URL(window.location.href);
        url.searchParams.set('onboarding', 'complete');
        window.history.replaceState(null, '', url);
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
      <label class="checkbox"><input type="checkbox" bind:checked={sonarrMonitorOnAdd} /> Monitor anime series on add</label>
      <label class="checkbox"><input type="checkbox" bind:checked={sonarrSearchOnAdd} /> Search for missing episodes on add</label>
    {:else if activeStep === 'Radarr'}
      <label class="checkbox"><input type="checkbox" bind:checked={enableRadarr} disabled={busy} /> Enable movie synchronization</label>
      {#if enableRadarr}
        <ServarrOnboardingStep service="radarr" bind:draft={radarr} bind:busy />
        <label class="checkbox"><input type="checkbox" bind:checked={radarrMonitorOnAdd} /> Monitor movies on add</label>
        <label class="checkbox"><input type="checkbox" bind:checked={radarrSearchOnAdd} /> Search for movies on add</label>
      {:else}
        <p>Continue to set up series synchronization only.</p>
      {/if}
    {:else if activeStep === 'Sync Preferences'}
      <AniListStatusPicker label="Series lists" bind:value={seriesStatuses} />
      {#if enableRadarr}
        <AniListStatusPicker label="Movie lists" bind:value={movieStatuses} />
      {/if}
      <p>Dropped titles are eligible for cleanup only. Completed and dropped cleanup rules still apply.</p>
      <label class="checkbox"><input type="checkbox" bind:checked={dryRun} /> Dry run mode</label>
      <p>Stage proposed changes for review before applying them to Sonarr or Radarr.</p>
      <label for="onboarding-interval">Sync interval (hours)</label>
      <input id="onboarding-interval" class="interval-input" type="number" min="1" step="1" required
        bind:value={interval} aria-invalid={!validInterval} aria-describedby="onboarding-interval-help" />
      <p id="onboarding-interval-help">Enter a positive whole number of hours.</p>
    {:else}
      <div class="summary">
        <div class="summary-group">
          <h2>AniList</h2>
          <dl>
            <dt>Username</dt><dd>{profile?.username ?? username.trim()}</dd>
            <dt>Series lists</dt><dd>{describeListStatuses(seriesStatuses)}</dd>
            {#if enableRadarr}<dt>Movie lists</dt><dd>{describeListStatuses(movieStatuses)}</dd>{/if}
          </dl>
        </div>
        {#each [
          { name: 'Sonarr', draft: sonarr, monitor: sonarrMonitorOnAdd, search: sonarrSearchOnAdd, enabled: true },
          { name: 'Radarr', draft: radarr, monitor: radarrMonitorOnAdd, search: radarrSearchOnAdd, enabled: enableRadarr },
        ] as service}
          <div class="summary-group">
            <h2>{service.name}</h2>
            {#if service.enabled}
              <dl>
                <dt>Server URL</dt><dd>{service.draft.baseUrl.trim()}</dd>
                <dt>Quality profile</dt><dd>{service.draft.options?.qualityProfiles.find(p => p.id === service.draft.profileId)?.name}</dd>
                <dt>Root folder</dt><dd>{service.draft.rootFolder}</dd>
                <dt>Monitor on add</dt><dd>{service.monitor ? 'On' : 'Off'}</dd>
                <dt>Search on add</dt><dd>{service.search ? 'On' : 'Off'}</dd>
              </dl>
            {:else}
              <p>Skipped</p>
            {/if}
          </div>
        {/each}
        <div class="summary-group">
          <h2>Sync preferences</h2>
          <dl>
            <dt>Interval</dt><dd>Every {interval} {interval === 1 ? 'hour' : 'hours'}</dd>
            <dt>Dry run</dt><dd>{dryRun ? 'On' : 'Off'}</dd>
          </dl>
        </div>
      </div>
      {#if config.restartRequired}
        <p>Previously saved settings require a restart. Finishing setup will restart the application.</p>
      {/if}
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
  .interval-input { max-width: 12rem; }
  .summary-group { padding: 14px 0; border-top: 1px solid var(--defaultBorderColor); }
  .summary-group h2 { margin: 0 0 8px; font-size: 1.1rem; color: var(--white); }
  .summary-group p { margin: 0; }
  dl { display: grid; grid-template-columns: minmax(0, 8rem) minmax(0, 1fr); gap: 6px 16px; margin: 0; }
  dt { font-weight: 600; }
  dd { margin: 0; overflow-wrap: anywhere; }
  footer { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; margin-top: 24px; }
</style>
