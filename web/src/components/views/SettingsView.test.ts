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

it('registers beforeunload while dirty and cleans it up on save, reset, and unmount', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );

  const dispatchBeforeUnload = () => {
    const event = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(event);
    return event;
  };

  // Clean state: should not prevent unload
  expect(dispatchBeforeUnload().defaultPrevented).toBe(false);

  // Edit port -> dirty -> should prevent unload
  const portInput = [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171')!;
  portInput.value = '7172';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(dispatchBeforeUnload().defaultPrevented).toBe(true);

  // Reset -> clean -> listener removed
  const resetBtn = [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Reset')!;
  resetBtn.click();
  await tick();
  expect(dispatchBeforeUnload().defaultPrevented).toBe(false);

  // Type password and confirmation, then clear password: formPasswordConfirm alone still counts as dirty
  const passwordInput = document.querySelector<HTMLInputElement>('input[placeholder="New password"]')!;
  passwordInput.value = 'secret';
  passwordInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  const confirmInput = document.querySelector<HTMLInputElement>('input[placeholder="Confirm new password"]')!;
  confirmInput.value = 'secret';
  confirmInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  passwordInput.value = '';
  passwordInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(dispatchBeforeUnload().defaultPrevented).toBe(true);

  // Restore matching password and save -> clean -> listener removed
  passwordInput.value = 'secret';
  passwordInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  const saveBtn = [...document.querySelectorAll('button')].find((b) => b.textContent?.includes('Save Changes'))!;
  saveBtn.click();
  await vi.waitFor(() => expect(api.updateConfig).toHaveBeenCalledTimes(1));
  await tick();
  expect(dispatchBeforeUnload().defaultPrevented).toBe(false);

  // Dirty again, then unmount -> listener removed
  portInput.value = '7173';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(dispatchBeforeUnload().defaultPrevented).toBe(true);

  component.$destroy();
  component = undefined;
  expect(dispatchBeforeUnload().defaultPrevented).toBe(false);
});

it('exposes saveConfig returning true on success and false on validation or API error', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  vi.mocked(api.updateConfig)
    .mockRejectedValueOnce(new Error('Backend unavailable'))
    .mockResolvedValueOnce({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );

  // Password mismatch -> returns false, no API call, error rendered
  const passwordInput = document.querySelector<HTMLInputElement>('input[placeholder="New password"]')!;
  passwordInput.value = 'secret-a';
  passwordInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  const confirmInput = document.querySelector<HTMLInputElement>('input[placeholder="Confirm new password"]')!;
  confirmInput.value = 'secret-b';
  confirmInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();

  await expect(component.saveConfig()).resolves.toBe(false);
  expect(api.updateConfig).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain('Passwords do not match');

  // Fix password mismatch, API error -> returns false, error rendered
  confirmInput.value = 'secret-a';
  confirmInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();

  await expect(component.saveConfig()).resolves.toBe(false);
  expect(api.updateConfig).toHaveBeenCalledTimes(1);
  expect(document.body.textContent).toContain('Backend unavailable');

  // Retry succeeds -> returns true
  await expect(component.saveConfig()).resolves.toBe(true);
  expect(api.updateConfig).toHaveBeenCalledTimes(2);
});

it('reuses an in-flight save promise when saveConfig is called concurrently', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  let resolveSave!: (value: { status: string }) => void;
  vi.mocked(api.updateConfig).mockImplementation(
    () => new Promise((resolve) => { resolveSave = resolve; }),
  );

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );

  const portInput = [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171')!;
  portInput.value = '7175';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));

  const first = component.saveConfig();
  const second = component.saveConfig();
  expect(api.updateConfig).toHaveBeenCalledTimes(1);

  resolveSave({ status: 'ok' });
  await expect(Promise.all([first, second])).resolves.toEqual([true, true]);
});

function findGroupControl<T extends HTMLElement>(labelText: string, selector: string): T {
  const group = [...document.querySelectorAll('.group')].find(
    (el) => el.querySelector('.label')?.textContent?.trim() === labelText,
  );
  if (!group) throw new Error(`FormGroup "${labelText}" not found`);
  const control = group.querySelector<T>(selector);
  if (!control) throw new Error(`Control "${selector}" in "${labelText}" not found`);
  return control;
}

it('ignores hidden proxy and SSL sub-settings when their parent toggle is disabled', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(
    baseConfig({ enableSsl: false, sslPort: 7272, useProxy: false, proxyHostname: 'saved.proxy', proxyPort: 8080 }),
  );
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );

  // Enable Proxy, edit hostname, then disable Proxy -> clean again, draft preserved if re-enabled
  const useProxyCheckbox = findGroupControl<HTMLElement>('Use Proxy', '[role="checkbox"]');
  useProxyCheckbox.click();
  await tick();
  const hostnameInput = findGroupControl<HTMLInputElement>('Hostname', 'input');
  hostnameInput.value = 'draft.proxy';
  hostnameInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(component.hasUnsavedChanges()).toBe(true);

  useProxyCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(false);
  expect(document.querySelector('[title="Field has unsaved changes"]')).toBeNull();

  // Re-enabling Proxy before saving preserves typed draft and marks dirty again
  useProxyCheckbox.click();
  await tick();
  expect(findGroupControl<HTMLInputElement>('Hostname', 'input').value).toBe('draft.proxy');
  expect(component.hasUnsavedChanges()).toBe(true);
  useProxyCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(false);

  // Enable Advanced -> enable SSL -> edit SSL Port -> disable SSL -> clean again
  const advancedBtn = [...document.querySelectorAll('button')].find((b) => b.textContent?.includes('Advanced'))!;
  advancedBtn.click();
  await tick();
  const enableSslCheckbox = findGroupControl<HTMLElement>('Enable SSL', '[role="checkbox"]');
  enableSslCheckbox.click();
  await tick();
  const sslPortInput = findGroupControl<HTMLInputElement>('SSL Port', 'input');
  sslPortInput.value = '7443';
  sslPortInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  expect(component.hasUnsavedChanges()).toBe(true);

  enableSslCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(false);

  // Saving an unrelated field does not leak hidden proxy or SSL edits
  const portInput = findGroupControl<HTMLInputElement>('Port Number', 'input');
  portInput.value = '7172';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  await expect(component.saveConfig()).resolves.toBe(true);

  expect(api.updateConfig).toHaveBeenCalledWith(
    expect.objectContaining({
      port: 7172,
      useProxy: false,
      proxyHostname: 'saved.proxy',
      enableSsl: false,
      sslPort: 7272,
    }),
  );
  expect(component.hasUnsavedChanges()).toBe(false);
});

it('preserves previously saved proxy sub-settings when disabling an already-enabled proxy', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(
    baseConfig({ useProxy: true, proxyType: 'Http', proxyHostname: 'corp.proxy', proxyPort: 8080 }),
  );
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === 'corp.proxy')).toBe(true),
  );

  // Edit hostname, then disable proxy -> still dirty because useProxy changed from true to false
  const hostnameInput = findGroupControl<HTMLInputElement>('Hostname', 'input');
  hostnameInput.value = 'temp.proxy';
  hostnameInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();

  findGroupControl<HTMLElement>('Use Proxy', '[role="checkbox"]').click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(true);

  await expect(component.saveConfig()).resolves.toBe(true);
  expect(api.updateConfig).toHaveBeenCalledWith(
    expect.objectContaining({
      useProxy: false,
      proxyHostname: 'corp.proxy',
    }),
  );
});

it('treats omitted optional proxy fields as clean after toggling Use Proxy on and off', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );

  const useProxyCheckbox = findGroupControl<HTMLElement>('Use Proxy', '[role="checkbox"]');
  useProxyCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(true);

  useProxyCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(false);
});

it('does not render editable config inputs before savedConfig loads or on load failure', async () => {
  let resolveConfig!: (cfg: AppConfig) => void;
  vi.mocked(api.getConfig).mockImplementation(
    () => new Promise((resolve) => { resolveConfig = resolve; }),
  );

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await tick();
  expect(document.querySelector('[role="status"]')?.textContent).toContain('Loading settings');
  expect(document.querySelector('input')).toBeNull();
  expect(component.hasUnsavedChanges()).toBe(false);

  resolveConfig(baseConfig());
  await vi.waitFor(() => expect(findGroupControl<HTMLInputElement>('Port Number', 'input').value).toBe('7171'));

  component.$destroy();
  component = undefined;

  vi.mocked(api.getConfig).mockRejectedValue(new Error('Config unavailable'));
  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() => expect(document.body.textContent).toContain('Config unavailable'));
  expect(document.querySelector('input')).toBeNull();
  expect(component.hasUnsavedChanges()).toBe(false);
});

it('preserves edits made while a save is in flight and saves the newer draft on a concurrent saveConfig call', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(baseConfig());
  const resolvers: Array<(value: { status: string }) => void> = [];
  vi.mocked(api.updateConfig).mockImplementation(
    () => new Promise((resolve) => { resolvers.push(resolve); }),
  );

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() => expect(findGroupControl<HTMLInputElement>('Port Number', 'input').value).toBe('7171'));

  const portInput = findGroupControl<HTMLInputElement>('Port Number', 'input');
  portInput.value = '7172';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();

  // Start initial toolbar save for 7172
  const firstSave = component.saveConfig();
  await vi.waitFor(() => expect(api.updateConfig).toHaveBeenCalledTimes(1));

  // Edit port to 7173 while 7172 is still in flight, then invoke saveConfig again (Save and leave)
  portInput.value = '7173';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));
  await tick();
  const secondSave = component.saveConfig();

  // Finish 7172 save -> live draft 7173 must survive and trigger a second save for 7173
  resolvers[0]({ status: 'ok' });
  await expect(firstSave).resolves.toBe(true);
  await vi.waitFor(() => expect(api.updateConfig).toHaveBeenCalledTimes(2));
  expect(portInput.value).toBe('7173');
  expect(api.updateConfig).toHaveBeenNthCalledWith(1, expect.objectContaining({ port: 7172 }));
  expect(api.updateConfig).toHaveBeenNthCalledWith(2, expect.objectContaining({ port: 7173 }));

  resolvers[1]({ status: 'ok' });
  await expect(secondSave).resolves.toBe(true);
  expect(component.hasUnsavedChanges()).toBe(false);
});

it('sends Http when Proxy is enabled over a stored empty proxyType and stays clean if disabled again', async () => {
  vi.mocked(api.getConfig).mockResolvedValue(
    baseConfig({ useProxy: false, proxyType: '', proxyHostname: 'proxy.local', proxyPort: 8080 }),
  );
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });

  component = new SettingsView({ target: document.body, props: { subTab: 'general' } });
  await vi.waitFor(() => expect(findGroupControl<HTMLInputElement>('Port Number', 'input').value).toBe('7171'));

  const useProxyCheckbox = findGroupControl<HTMLElement>('Use Proxy', '[role="checkbox"]');
  useProxyCheckbox.click();
  await tick();
  expect(findGroupControl<HTMLSelectElement>('Proxy Type', 'select').value).toBe('Http');

  // Disabling again without saving returns to clean
  useProxyCheckbox.click();
  await tick();
  expect(component.hasUnsavedChanges()).toBe(false);

  // Re-enabling and saving sends proxyType: 'Http' matching the displayed select option
  useProxyCheckbox.click();
  await tick();
  await expect(component.saveConfig()).resolves.toBe(true);
  expect(api.updateConfig).toHaveBeenCalledWith(
    expect.objectContaining({
      useProxy: true,
      proxyType: 'Http',
    }),
  );
});
