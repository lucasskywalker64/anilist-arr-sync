// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { api, waitForRestart } from '../../lib/api';
import type { AppConfig } from '../../lib/types';
import OnboardingWizard from './OnboardingWizard.svelte';

vi.mock('../../lib/api', () => ({
  api: {
    testAniList: vi.fn(), testServarr: vi.fn(), updateConfig: vi.fn(),
    restart: vi.fn(), startSync: vi.fn(),
  },
  waitForRestart: vi.fn(),
  getBaseUrl: () => '',
}));

const config = {
  aniListUsername: 'Viewer', sonarrUrl: 'http://sonarr', sonarrApiKey: '******',
  sonarrMonitorOnAdd: true, sonarrQualityProfileId: 7, sonarrRootFolderPath: '/anime',
  radarrUrl: '', firstRunDryRun: true, syncIntervalHours: 4,
} as AppConfig;

let component: OnboardingWizard | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  vi.resetAllMocks();
  window.history.replaceState(null, '', '/');
});

function button(label: string) {
  const match = [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === label);
  if (!match) throw new Error(`Missing button: ${label}`);
  return match;
}

function checkbox(label: string) {
  const match = [...document.querySelectorAll('label')].find(item => item.textContent?.trim() === label)?.querySelector('input');
  if (!match) throw new Error(`Missing checkbox: ${label}`);
  return match;
}

async function click(label: string) {
  button(label).click();
  await tick();
}

function mount(overrides: Partial<AppConfig> = {}) {
  component = new OnboardingWizard({ target: document.body, props: { config: { ...config, ...overrides } } });
  return component;
}

it('dismisses setup without saving', async () => {
  const dismissed = vi.fn();
  mount().$on('dismiss', dismissed);
  await click('Configure Later');
  expect(dismissed).toHaveBeenCalledOnce();
  expect(api.updateConfig).not.toHaveBeenCalled();
});

it('shows invalid credentials inline and requires the changed account to be verified again', async () => {
  vi.mocked(api.testAniList).mockRejectedValueOnce(new Error('AniList profile not found'))
    .mockResolvedValueOnce({ username: 'Viewer', avatarUrl: '', animeCount: 42, mangaCount: 2 });
  mount();
  await click('Next');
  await click('Verify Account');
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('profile not found');
  expect(button('Next').disabled).toBe(true);
  await click('Verify Account');
  expect(document.body.textContent).toContain('42 anime');
  expect(button('Next').disabled).toBe(false);
  const input = document.querySelector<HTMLInputElement>('#onboarding-anilist')!;
  input.value = 'Different';
  input.dispatchEvent(new Event('input'));
  await tick();
  expect(button('Next').disabled).toBe(true);
});

it('saves series-only settings once, waits for restart, and starts a forced dry run', async () => {
  vi.mocked(api.testAniList).mockResolvedValue({ username: 'Viewer', avatarUrl: '', animeCount: 42, mangaCount: 2 });
  vi.mocked(api.testServarr).mockResolvedValue({ qualityProfiles: [{ id: 7, name: 'HD' }], rootFolders: [{ id: 2, path: '/anime' }] });
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok', restartRequired: true });
  vi.mocked(api.restart).mockImplementation(async () => {
    expect(window.location.search).toBe('?onboarding=complete');
    return { restarting: true, instanceId: 'old', reconnectUrl: `${window.location.origin}/` };
  });
  let ready!: () => void;
  vi.mocked(waitForRestart).mockReturnValue(new Promise(resolve => { ready = resolve; }));
  vi.mocked(api.startSync).mockResolvedValue({ status: 'started' });
  const finished = vi.fn();
  mount({ firstRunDryRun: false, restartRequired: true }).$on('finish', finished);

  await click('Next');
  await click('Verify Account');
  await click('Next');
  await click('Test Connection');
  expect(api.testServarr).toHaveBeenCalledWith('sonarr', 'http://sonarr', '');
  expect(document.body.textContent).toContain('Connected to Sonarr');
  await click('Next');
  await click('Next');
  await click('Next');
  expect(document.body.textContent).toContain('Finishing setup will restart the application.');
  await click('Finish Setup');
  expect(api.updateConfig).toHaveBeenCalledOnce();
  expect(api.updateConfig).toHaveBeenCalledWith(expect.objectContaining({
    aniListUsername: 'Viewer', sonarrQualityProfileId: 7, sonarrRootFolderPath: '/anime',
    sonarrSearchOnAdd: false,
    radarrUrl: '', radarrApiKey: '', firstRunDryRun: false, syncIntervalHours: 4, wizardCompleted: true,
  }));
  expect(button('Start Initial Dry Run').disabled).toBe(true);
  expect(api.startSync).not.toHaveBeenCalled();
  ready();
  await vi.waitFor(() => expect(button('Start Initial Dry Run').disabled).toBe(false));
  await click('Start Initial Dry Run');
  expect(api.startSync).toHaveBeenCalledWith(true);
  expect(finished).toHaveBeenCalledOnce();
});

it('reviews and saves Radarr preferences and a custom whole-hour sync interval', async () => {
  vi.mocked(api.testAniList).mockResolvedValue({ username: 'Viewer', avatarUrl: '', animeCount: 42, mangaCount: 2 });
  vi.mocked(api.testServarr)
    .mockResolvedValueOnce({ qualityProfiles: [{ id: 7, name: 'HD' }], rootFolders: [{ id: 2, path: '/anime' }] })
    .mockRejectedValueOnce(new Error('could not load system status from Radarr (HTTP 401)'))
    .mockResolvedValueOnce({ qualityProfiles: [{ id: 11, name: 'Movies' }], rootFolders: [{ id: 3, path: '/movies' }] });
  vi.mocked(api.updateConfig).mockResolvedValue({ status: 'ok' });
  mount({ radarrUrl: 'http://radarr', radarrApiKey: '******', radarrQualityProfileId: 1, radarrRootFolderPath: '' });

  await click('Next');
  await click('Verify Account');
  await click('Next');
  await click('Test Connection');
  const sonarrSearch = checkbox('Search for missing episodes on add');
  expect(sonarrSearch.checked).toBe(false);
  sonarrSearch.click();
  await click('Next');
  expect(button('Next').disabled).toBe(true);
  await click('Test Connection');
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('HTTP 401');
  expect(button('Next').disabled).toBe(true);
  await click('Test Connection');
  const monitor = checkbox('Monitor movies on add');
  expect(monitor.checked).toBe(true);
  monitor.click();
  const radarrSearch = checkbox('Search for movies on add');
  expect(radarrSearch.checked).toBe(false);
  radarrSearch.click();
  await click('Next');
  const [series, movies] = [...document.querySelectorAll('fieldset')];
  series.querySelector<HTMLInputElement>('input[value="PLANNING"]')!.click();
  movies.querySelector<HTMLInputElement>('input[value="REPEATING"]')!.click();
  await tick();
  movies.querySelector<HTMLInputElement>('input[value="CURRENT"]')!.click();
  const interval = document.querySelector<HTMLInputElement>('#onboarding-interval')!;
  expect(interval.valueAsNumber).toBe(4);
  for (const invalid of ['', '0', '3.5']) {
    interval.value = invalid;
    interval.dispatchEvent(new Event('input'));
    await tick();
    expect(button('Next').disabled).toBe(true);
  }
  interval.value = '7';
  interval.dispatchEvent(new Event('input'));
  await tick();
  await click('Next');
  const groups = [...document.querySelectorAll('.summary-group')];
  expect(groups.map(group => group.querySelector('h2')?.textContent)).toEqual(['AniList', 'Sonarr', 'Radarr', 'Sync preferences']);
  expect([...groups[0].querySelectorAll('dd')].map(value => value.textContent)).toEqual(['Viewer', 'Watching, Planning', 'Rewatching']);
  expect([...groups[1].querySelectorAll('dd')].at(-1)?.textContent).toBe('On');
  expect([...groups[2].querySelectorAll('dd')].map(value => value.textContent)).toEqual(['http://radarr', 'Movies', '/movies', 'Off', 'On']);
  expect(groups[3].textContent).toContain('Every 7 hours');
  expect(document.body.textContent).not.toContain('Finishing setup will restart the application.');
  await click('Finish Setup');
  expect(api.updateConfig).toHaveBeenCalledWith(expect.objectContaining({
    radarrUrl: 'http://radarr', radarrQualityProfileId: 11, radarrRootFolderPath: '/movies',
    radarrMonitorOnAdd: false, sonarrSearchOnAdd: true, radarrSearchOnAdd: true,
    aniListTvStatus: 'CURRENT,PLANNING', aniListMovieStatus: 'REPEATING',
    firstRunDryRun: true, syncIntervalHours: 7,
  }));
  expect(api.restart).not.toHaveBeenCalled();
});
