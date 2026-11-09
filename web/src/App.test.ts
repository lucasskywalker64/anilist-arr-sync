// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import App from './App.svelte';

// Vitest resolves Svelte's SSR entry point, where onMount is a no-op.
vi.mock('svelte', async (importOriginal) => ({
  ...await importOriginal<typeof import('svelte')>(),
  onMount: (await vi.importActual<typeof import('svelte')>('svelte/internal')).onMount,
}));

let component: App | undefined;
afterEach(() => {
  component?.$destroy();
  document.body.replaceChildren();
  sessionStorage.clear();
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

it('opens setup before the dashboard and can reopen it from General settings after dismissal', async () => {
  window.history.replaceState(null, '', '/settings');
  let accountCreated = false;
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.endsWith('/auth/setup')) {
      accountCreated = true;
      return Response.json({ username: 'admin' }, { status: 201 });
    }
    if (url.endsWith('/auth/user')) {
      return accountCreated ? Response.json({ authenticated: true, onboardingRequired: true })
        : Response.json({ setupRequired: true }, { status: 401 });
    }
    if (url.endsWith('/config')) return Response.json({
      instanceName: 'Test', aniListUsername: '', sonarrUrl: '', radarrUrl: '', syncIntervalHours: 4, firstRunDryRun: true,
    });
    return Response.json([]);
  }));
  component = new App({ target: document.body });

  const findButton = (label: string) => [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === label);
  await vi.waitFor(() => expect(findButton('Complete Setup')).toBeDefined());
  for (const [id, value] of Object.entries({ username: 'admin', password: 'test-password', confirmPassword: 'test-password' })) {
    const input = document.querySelector<HTMLInputElement>(`#${id}`)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }
  findButton('Complete Setup')!.click();
  await vi.waitFor(() => expect(findButton('Configure Later')).toBeDefined());
  expect(findButton('Open Setup Wizard')).toBeUndefined();
  findButton('Configure Later')!.click();
  await vi.waitFor(() => expect(findButton('Open Setup Wizard')?.disabled).toBe(false));
  findButton('Open Setup Wizard')!.click();
  await vi.waitFor(() => expect(findButton('Configure Later')).toBeDefined());
});

it('keeps completed setup out of Settings and saves Servarr defaults there', async () => {
  window.history.replaceState(null, '', '/settings');
  const config = {
    wizardCompleted: true, port: 7171, aniListUsername: 'Viewer',
    aniListTvStatus: 'CURRENT', aniListMovieStatus: 'CURRENT',
    sonarrUrl: 'http://sonarr', sonarrApiKey: '******', sonarrQualityProfileId: 7,
    sonarrRootFolderPath: '/anime', sonarrMonitorOnAdd: true, sonarrSearchOnAdd: false,
    radarrUrl: 'http://radarr', radarrApiKey: '******', radarrQualityProfileId: 7,
    radarrRootFolderPath: '/anime', radarrMonitorOnAdd: true, radarrSearchOnAdd: false,
  };
  let saved: unknown;
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) {
      if (init?.method === 'PUT') {
        saved = JSON.parse(String(init.body));
        return Response.json({ status: 'ok' });
      }
      return Response.json(config);
    }
    if (url.endsWith('/servarr/test')) {
      const connection = JSON.parse(String(init?.body));
      expect(connection).toEqual({ type: connection.type, baseUrl: `http://${connection.type}`, apiKey: '' });
      return Response.json({
        qualityProfiles: [{ id: 7, name: 'HD' }, { id: 8, name: 'UHD' }],
        rootFolders: [{ id: 2, path: '/anime' }, { id: 3, path: '/new-anime' }],
      });
    }
    return Response.json([]);
  }));
  component = new App({ target: document.body });

  const findButton = (label: string) => [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === label);
  await vi.waitFor(() => expect([...document.querySelectorAll('input')].some(input => input.value === '7171')).toBe(true));
  expect(findButton('Open Setup Wizard')).toBeUndefined();

  window.history.replaceState(null, '', '/settings/servarr');
  window.dispatchEvent(new PopStateEvent('popstate'));
  for (const service of ['Sonarr', 'Radarr']) {
    const prefix = service.toLowerCase();
    const label = `Load ${service} profiles and folders`;
    await vi.waitFor(() => expect(findButton(label)?.disabled).toBe(false));
    findButton(label)!.click();
    await vi.waitFor(() => expect(document.querySelector(`select#${prefix}-settings-profile`)).not.toBeNull());
    for (const [field, value] of Object.entries({ profile: '8', folder: '/new-anime' })) {
      const select = document.querySelector<HTMLSelectElement>(`#${prefix}-settings-${field}`)!;
      select.value = value;
      select.dispatchEvent(new Event('change'));
    }
    document.querySelector<HTMLElement>(`[aria-label="${service} monitor on add"]`)!.click();
    document.querySelector<HTMLElement>(`[aria-label="${service} search on add"]`)!.click();
  }
  window.history.replaceState(null, '', '/settings/sync');
  window.dispatchEvent(new PopStateEvent('popstate'));
  await vi.waitFor(() => expect(document.querySelector('input[value="PLANNING"]')).not.toBeNull());
  document.querySelector<HTMLInputElement>('input[value="PLANNING"]')!.click();
  await vi.waitFor(() => expect(findButton('Save Changes')?.disabled).toBe(false));
  findButton('Save Changes')!.click();
  await vi.waitFor(() => expect(saved).toMatchObject({
    aniListTvStatus: 'CURRENT,PLANNING', aniListMovieStatus: 'CURRENT',
    sonarrQualityProfileId: 8, sonarrRootFolderPath: '/new-anime', sonarrMonitorOnAdd: false, sonarrSearchOnAdd: true,
    radarrQualityProfileId: 8, radarrRootFolderPath: '/new-anime', radarrMonitorOnAdd: false, radarrSearchOnAdd: true,
  }));
});

it('restores the initial dry-run choice after navigating to a new listener', async () => {
  window.history.replaceState(null, '', '/?onboarding=complete');
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) return Response.json({ aniListUsername: 'Viewer', sonarrUrl: 'http://sonarr', radarrUrl: '', syncIntervalHours: 4 });
    return Response.json([]);
  }));
  component = new App({ target: document.body });

  const findButton = (label: string) => [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === label);
  await vi.waitFor(() => expect(findButton('Start Initial Dry Run')?.disabled).toBe(false));
  findButton('Go to Dashboard')!.click();
  expect(window.location.search).toBe('');
});

it('offers to retry a pending setup restart after reloading', async () => {
  window.history.replaceState(null, '', '/?onboarding=complete');
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) return Response.json({ aniListUsername: 'Viewer', sonarrUrl: 'http://sonarr', radarrUrl: '', syncIntervalHours: 4, restartRequired: true });
    if (url.endsWith('/system/restart')) return Response.json({ instanceId: 'old', reconnectUrl: `${window.location.origin}/` });
    if (url.endsWith('/health')) return Response.json({ status: 'ok', instanceId: 'new' });
    return Response.json([]);
  });
  vi.stubGlobal('fetch', fetch);
  component = new App({ target: document.body });

  const findButton = (label: string) => [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === label);
  await vi.waitFor(() => expect(findButton('Retry Restart')).toBeDefined());
  expect(findButton('Start Initial Dry Run')?.disabled).toBe(true);
  findButton('Retry Restart')!.click();
  await vi.waitFor(() => expect(findButton('Start Initial Dry Run')?.disabled).toBe(false));
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/config'))).toHaveLength(1);
});
