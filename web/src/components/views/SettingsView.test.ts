// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api } from '../../lib/api';
import SettingsView from './SettingsView.svelte';
import type { AppConfig } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  api: {
    getConfig: vi.fn(),
    updateConfig: vi.fn(),
    testServarr: vi.fn(),
    getNotifications: vi.fn(),
  },
}));

let component: SettingsView | undefined;

afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.resetAllMocks();
});

function baseConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    bindAddress: '0.0.0.0',
    port: 7171,
    urlBase: '',
    enableSsl: false,
    sslPort: 7272,
    authenticationMethod: 'Forms',
    authenticationRequired: 'DisabledForLocalAddresses',
    apiKey: 'test-key',
    logLevel: 'Info',
    instanceName: 'AniList Arr Sync',
    wizardCompleted: true,
    aniListUsername: 'tester',
    aniListTvStatus: 'CURRENT',
    aniListMovieStatus: 'CURRENT',
    syncIntervalHours: 4,
    dateToleranceDays: 14,
    confidenceThreshold: 0.6,
    unmonitorDropped: false,
    firstRunDryRun: false,
    sonarrUrl: 'http://localhost:8989',
    sonarrApiKey: '******',
    sonarrQualityProfileId: 1,
    sonarrQualityProfileOverrides: {},
    sonarrRootFolderPath: '/tv',
    sonarrMonitorOnAdd: true,
    sonarrSearchOnAdd: false,
    radarrUrl: 'http://localhost:7878',
    radarrApiKey: '******',
    radarrQualityProfileId: 2,
    radarrQualityProfileOverrides: {},
    radarrRootFolderPath: '/movies',
    radarrMonitorOnAdd: true,
    radarrSearchOnAdd: false,
    tagName: 'anilist-sync',
    ...overrides,
  };
}

it('adds, edits, and removes Sonarr country profile overrides using loaded profiles', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig({ sonarrQualityProfileOverrides: { TW: 4 } }));
  vi.mocked(api.testServarr).mockResolvedValue({
    qualityProfiles: [
      { id: 1, name: 'Anime (JP)' },
      { id: 4, name: 'Donghua' },
      { id: 5, name: 'Korean' },
    ],
    rootFolders: [{ id: 1, path: '/tv' }],
  });
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'servarr' } });
  await vi.waitFor(() =>
    expect((document.querySelector('#sonarr-settings-url') as HTMLInputElement | null)?.value).toBe('http://localhost:8989'),
  );

  // Load Sonarr quality profiles
  const loadBtn = [...document.querySelectorAll('button')].find((b) =>
    b.textContent?.includes('Load Sonarr profiles and folders'),
  )!;
  loadBtn.click();
  await vi.waitFor(() => expect(document.querySelector('#sonarr-override-profile')?.tagName).toBe('SELECT'));

  // Add CN -> Donghua (4) using lowercase input
  const countryInput = document.querySelector<HTMLInputElement>('#sonarr-override-country')!;
  expect(countryInput.getAttribute('aria-label')).toBe('Add Sonarr country code');
  countryInput.value = 'cn';
  countryInput.dispatchEvent(new Event('input', { bubbles: true }));
  const profileSelect = document.querySelector<HTMLSelectElement>('#sonarr-override-profile')!;
  expect(profileSelect.getAttribute('aria-label')).toBe('Add Sonarr quality profile');
  profileSelect.value = '4';
  profileSelect.dispatchEvent(new Event('change', { bubbles: true }));
  await tick();

  document.querySelector<HTMLButtonElement>('[aria-label="Add Sonarr country override"]')!.click();
  await tick();
  expect(document.body.textContent).toContain('CN');
  expect(document.body.textContent).toContain('Donghua');

  // Edit CN -> KR with Korean (5)
  document.querySelector<HTMLButtonElement>('[aria-label="Edit Sonarr override CN"]')!.click();
  await tick();
  const editCountry = document.querySelector<HTMLInputElement>('#sonarr-edit-override-country')!;
  expect(editCountry.getAttribute('aria-label')).toBe('Edit Sonarr override country for CN');
  editCountry.value = 'kr';
  editCountry.dispatchEvent(new Event('input', { bubbles: true }));
  const editProfile = document.querySelector<HTMLSelectElement>('#sonarr-edit-override-profile')!;
  expect(editProfile.getAttribute('aria-label')).toBe('Edit Sonarr override profile for CN');
  editProfile.value = '5';
  editProfile.dispatchEvent(new Event('change', { bubbles: true }));
  await tick();
  document.querySelector<HTMLButtonElement>('[aria-label="Save Sonarr override edit"]')!.click();
  await tick();

  // Remove existing TW override
  document.querySelector<HTMLButtonElement>('[aria-label="Remove Sonarr override TW"]')!.click();
  await tick();

  // Save configuration
  const saveBtn = [...document.querySelectorAll('button')].find((b) =>
    b.textContent?.includes('Save Changes'),
  )!;
  saveBtn.click();

  await vi.waitFor(() =>
    expect(api.updateConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        sonarrQualityProfileOverrides: { KR: 5 },
        radarrQualityProfileOverrides: {},
      }),
    ),
  );
});

it('supports numeric profile ID entry for Radarr overrides and rejects invalid country codes', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'servarr' } });
  await vi.waitFor(() =>
    expect((document.querySelector('#radarr-settings-url') as HTMLInputElement | null)?.value).toBe('http://localhost:7878'),
  );

  // Invalid 3-letter country code should show validation message and not add
  const countryInput = document.querySelector<HTMLInputElement>('#radarr-override-country')!;
  countryInput.value = 'CHN';
  countryInput.dispatchEvent(new Event('input', { bubbles: true }));
  const profileInput = document.querySelector<HTMLInputElement>('#radarr-override-profile')!;
  profileInput.value = '6';
  profileInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();

  document.querySelector<HTMLButtonElement>('[aria-label="Add Radarr country override"]')!.click();
  await tick();
  expect(document.body.textContent).toContain('2-letter');

  // Fix country code to CN and add
  countryInput.value = 'cn';
  countryInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  document.querySelector<HTMLButtonElement>('[aria-label="Add Radarr country override"]')!.click();
  await tick();

  const saveBtn = [...document.querySelectorAll('button')].find((b) =>
    b.textContent?.includes('Save Changes'),
  )!;
  saveBtn.click();

  await vi.waitFor(() =>
    expect(api.updateConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        radarrQualityProfileOverrides: { CN: 6 },
      }),
    ),
  );
});

it('disables override controls while a configuration save is pending', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig({ sonarrQualityProfileOverrides: { CN: 4 } }));
  let resolveSave!: (value: { status: string }) => void;
  vi.mocked(api.updateConfig).mockImplementation(
    () => new Promise((resolve) => { resolveSave = resolve; }),
  );

  component = new SettingsView({ target: document.body, props: { subTab: 'servarr' } });
  await vi.waitFor(() =>
    expect((document.querySelector('#sonarr-settings-url') as HTMLInputElement | null)?.value).toBe('http://localhost:8989'),
  );

  // Add an override to make the form dirty
  const countryInput = document.querySelector<HTMLInputElement>('#sonarr-override-country')!;
  countryInput.value = 'KR';
  countryInput.dispatchEvent(new Event('input', { bubbles: true }));
  const profileInput = document.querySelector<HTMLInputElement>('#sonarr-override-profile')!;
  profileInput.value = '5';
  profileInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  document.querySelector<HTMLButtonElement>('[aria-label="Add Sonarr country override"]')!.click();
  await tick();

  // Start saving and verify override controls are disabled while in flight
  const saveBtn = [...document.querySelectorAll('button')].find((b) =>
    b.textContent?.includes('Save Changes'),
  )!;
  saveBtn.click();
  await tick();

  expect(document.querySelector<HTMLInputElement>('#sonarr-override-country')!.disabled).toBe(true);
  expect(document.querySelector<HTMLInputElement>('#sonarr-override-profile')!.disabled).toBe(true);
  expect(document.querySelector<HTMLButtonElement>('[aria-label="Edit Sonarr override CN"]')!.disabled).toBe(true);
  expect(document.querySelector<HTMLButtonElement>('[aria-label="Add Sonarr country override"]')!.disabled).toBe(true);

  resolveSave({ status: 'ok' });
  await vi.waitFor(() =>
    expect(document.querySelector<HTMLInputElement>('#sonarr-override-country')!.disabled).toBe(false),
  );
});
