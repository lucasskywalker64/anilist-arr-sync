// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import App from './App.svelte';

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

async function mountSettingsApp(options: {
  onSave?: (payload: Record<string, unknown>) => Promise<Response> | Response;
} = {}) {
  window.history.replaceState(null, '', '/settings/general');
  const config = {
    wizardCompleted: true,
    port: 7171,
    aniListUsername: 'Viewer',
    aniListTvStatus: 'CURRENT',
    aniListMovieStatus: 'CURRENT',
    sonarrUrl: 'http://sonarr',
    sonarrApiKey: '******',
    sonarrQualityProfileId: 7,
    sonarrRootFolderPath: '/anime',
    sonarrMonitorOnAdd: true,
    sonarrSearchOnAdd: false,
    radarrUrl: 'http://radarr',
    radarrApiKey: '******',
    radarrQualityProfileId: 7,
    radarrRootFolderPath: '/anime',
    radarrMonitorOnAdd: true,
    radarrSearchOnAdd: false,
  };
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) {
      if (init?.method === 'PUT') {
        const payload = JSON.parse(String(init.body));
        return options.onSave ? await options.onSave(payload) : Response.json({ status: 'ok' });
      }
      return Response.json(config);
    }
    return Response.json([]);
  }));
  component = new App({ target: document.body });

  const findButton = (label: string) =>
    [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
  const findNavLink = (label: string) =>
    [...document.querySelectorAll('aside a')].find((a) => a.textContent?.trim() === label) as HTMLAnchorElement | undefined;

  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );
  const portInput = () => [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171' || i.type === 'number')!;
  return { findButton, findNavLink, portInput };
}

it('allows immediate navigation when clean, stays across Settings sub-tabs, and guards sidebar leave with keep-editing dismissals', async () => {
  const { findButton, findNavLink, portInput } = await mountSettingsApp();

  // Clean navigation leaves immediately without modal
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();

  // Return to Settings and edit port
  findNavLink('Settings')!.click();
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );
  const input = portInput();
  input.value = '8080';
  input.dispatchEvent(new Event('input', { bubbles: true }));

  // Switching Settings sub-tabs while dirty does not trigger modal and preserves edits
  findNavLink('Sync & AniList')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/settings/sync'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  findNavLink('General')!.click();
  await vi.waitFor(() => expect(portInput().value).toBe('8080'));

  // Clicking non-settings sidebar link opens confirmation modal
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(document.querySelector('[role="dialog"]')?.textContent).toMatch(/unsaved/i);
  expect(findButton('Keep editing')).toBeDefined();
  expect(findButton('Leave without saving')).toBeDefined();
  expect(findButton('Save and leave')).toBeDefined();

  // Keep editing button dismisses modal and keeps edits
  findButton('Keep editing')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(portInput().value).toBe('8080');

  // Escape key dismisses modal
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');

  // Close button dismisses modal
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  document.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');

  // Backdrop click dismisses modal
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  document.querySelector<HTMLElement>('.modalBackdrop')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(portInput().value).toBe('8080');
});

it('intercepts popstate and hashchange navigation, keeps URL aligned on keep editing, and navigates on leave without saving', async () => {
  const onSave = vi.fn(() => Response.json({ status: 'ok' }));
  const { findButton, portInput } = await mountSettingsApp({ onSave });

  const input = portInput();
  input.value = '8081';
  input.dispatchEvent(new Event('input', { bubbles: true }));

  // Simulate browser back/forward via popstate
  window.history.replaceState(null, '', '/overrides');
  window.dispatchEvent(new PopStateEvent('popstate'));
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  expect(window.location.pathname).toBe('/settings/general');

  // Keep editing keeps URL aligned with Settings and edits intact
  findButton('Keep editing')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(portInput().value).toBe('8081');

  // Simulate hashchange navigation away from Settings
  window.history.replaceState(null, '', '/#/library');
  window.dispatchEvent(new HashChangeEvent('hashchange'));
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(window.location.hash).toBe('');

  // Leave without saving discards edits, navigates to target, and cleans up beforeunload
  findButton('Leave without saving')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(onSave).not.toHaveBeenCalled();

  const unloadEvent = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(unloadEvent);
  expect(unloadEvent.defaultPrevented).toBe(false);
});

it('disables modal buttons while saving and navigates when Save and leave succeeds', async () => {
  let resolveSave!: (res: Response) => void;
  let savedPayload: Record<string, unknown> | undefined;
  const { findButton, findNavLink, portInput } = await mountSettingsApp({
    onSave: (payload) => {
      savedPayload = payload;
      return new Promise<Response>((resolve) => {
        resolveSave = resolve;
      });
    },
  });

  const input = portInput();
  input.value = '9090';
  input.dispatchEvent(new Event('input', { bubbles: true }));

  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());

  findButton('Save and leave')!.click();
  await vi.waitFor(() => expect(findButton('Save and leave')?.disabled).toBe(true));
  expect(findButton('Keep editing')?.disabled).toBe(true);
  expect(findButton('Leave without saving')?.disabled).toBe(true);

  // Escape while saving should not dismiss the modal
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();

  resolveSave(Response.json({ status: 'ok' }));
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(savedPayload).toMatchObject({ port: 9090 });
});

it('stays in Settings with edits intact and surfaces errors when Save and leave fails', async () => {
  let shouldFailApi = true;
  const { findButton, findNavLink } = await mountSettingsApp({
    onSave: () =>
      shouldFailApi
        ? Response.json({ error: 'Config save rejected' }, { status: 500 })
        : Response.json({ status: 'ok' }),
  });

  // Trigger password mismatch failure
  const passwordInput = document.querySelector<HTMLInputElement>('input[placeholder="New password"]')!;
  passwordInput.value = 'pass-1';
  passwordInput.dispatchEvent(new Event('input', { bubbles: true }));
  await vi.waitFor(() => expect(document.querySelector('input[placeholder="Confirm new password"]')).not.toBeNull());
  const confirmInput = document.querySelector<HTMLInputElement>('input[placeholder="Confirm new password"]')!;
  confirmInput.value = 'pass-2';
  confirmInput.dispatchEvent(new Event('input', { bubbles: true }));

  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  findButton('Save and leave')!.click();

  await vi.waitFor(() => expect(document.body.textContent).toContain('Passwords do not match'));
  expect(window.location.pathname).toBe('/settings/general');
  expect(passwordInput.value).toBe('pass-1');
  expect(confirmInput.value).toBe('pass-2');

  // Fix password mismatch, trigger API failure
  confirmInput.value = 'pass-1';
  confirmInput.dispatchEvent(new Event('input', { bubbles: true }));

  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  findButton('Save and leave')!.click();

  await vi.waitFor(() => expect(document.body.textContent).toContain('Config save rejected'));
  expect(window.location.pathname).toBe('/settings/general');
  expect(passwordInput.value).toBe('pass-1');

  // Allow API save to succeed on retry
  shouldFailApi = false;
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  findButton('Save and leave')!.click();

  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
});

it('preserves browser history stack across actual Back navigation when leaving Settings', async () => {
  window.history.replaceState(null, '', '/activity/history');
  const config = {
    wizardCompleted: true,
    port: 7171,
    aniListUsername: 'Viewer',
    aniListTvStatus: 'CURRENT',
    aniListMovieStatus: 'CURRENT',
    sonarrUrl: 'http://sonarr',
    sonarrApiKey: '******',
    sonarrQualityProfileId: 7,
    sonarrRootFolderPath: '/anime',
    sonarrMonitorOnAdd: true,
    sonarrSearchOnAdd: false,
    radarrUrl: 'http://radarr',
    radarrApiKey: '******',
    radarrQualityProfileId: 7,
    radarrRootFolderPath: '/anime',
    radarrMonitorOnAdd: true,
    radarrSearchOnAdd: false,
  };
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) return Response.json(config);
    return Response.json([]);
  }));
  component = new App({ target: document.body });

  const findButton = (label: string) =>
    [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
  const findNavLink = (label: string) =>
    [...document.querySelectorAll('aside a')].find((a) => a.textContent?.trim() === label) as HTMLAnchorElement | undefined;

  await vi.waitFor(() => expect(findNavLink('Library')).toBeDefined());

  // Visit History -> Library -> Settings
  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  findNavLink('Settings')!.click();
  await vi.waitFor(() =>
    expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
  );
  expect(window.location.pathname).toBe('/settings/general');

  const portInput = [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171')!;
  portInput.value = '8085';
  portInput.dispatchEvent(new Event('input', { bubbles: true }));

  // Press Back -> prompt opens and URL restores to Settings without replacing history
  window.history.back();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  await vi.waitFor(() => expect(window.location.pathname).toBe('/settings/general'));

  // Keep editing stays on Settings
  findButton('Keep editing')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
  expect(window.location.pathname).toBe('/settings/general');
  expect(portInput.value).toBe('8085');

  // Press Back again -> Leave without saving -> lands on Library
  window.history.back();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
  await vi.waitFor(() => expect(window.location.pathname).toBe('/settings/general'));
  findButton('Leave without saving')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();

  // Next Back goes to History instead of bouncing back to Settings
  window.history.back();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/activity/history'));
});

it('clears a pending leave prompt when SettingsView is unmounted by a 401 and re-authenticated', async () => {
  vi.useFakeTimers();
  try {
    window.history.replaceState(null, '', '/settings/general');
    let expired = false;
    const config = {
      wizardCompleted: true,
      port: 7171,
      authenticationMethod: 'Forms',
      aniListUsername: 'Viewer',
      aniListTvStatus: 'CURRENT',
      aniListMovieStatus: 'CURRENT',
      sonarrUrl: 'http://sonarr',
      sonarrApiKey: '******',
      sonarrQualityProfileId: 7,
      sonarrRootFolderPath: '/anime',
      sonarrMonitorOnAdd: true,
      sonarrSearchOnAdd: false,
      radarrUrl: 'http://radarr',
      radarrApiKey: '******',
      radarrQualityProfileId: 7,
      radarrRootFolderPath: '/anime',
      radarrMonitorOnAdd: true,
      radarrSearchOnAdd: false,
    };
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url.endsWith('/auth/login')) {
        expired = false;
        return Response.json({ username: 'admin' });
      }
      if (url.endsWith('/auth/user')) {
        return expired
          ? Response.json({ error: 'unauthorized' }, { status: 401 })
          : Response.json({ authenticated: true, wizardCompleted: true });
      }
      if (expired) {
        return Response.json({ error: 'unauthorized' }, { status: 401 });
      }
      if (url.endsWith('/config')) return Response.json(config);
      return Response.json([]);
    }));
    component = new App({ target: document.body });

    const findButton = (label: string) =>
      [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
    const findNavLink = (label: string) =>
      [...document.querySelectorAll('aside a')].find((a) => a.textContent?.trim() === label) as HTMLAnchorElement | undefined;

    await vi.waitFor(() =>
      expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
    );
    const portInput = [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171')!;
    portInput.value = '8086';
    portInput.dispatchEvent(new Event('input', { bubbles: true }));

    findNavLink('Library')!.click();
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());

    // Background poll hits 401 -> LoginModal replaces SettingsView
    expired = true;
    await vi.advanceTimersByTimeAsync(10000);
    await vi.waitFor(() => expect(findButton('Sign In')).toBeDefined());

    // Sign back in -> fresh SettingsView mounts without reopening stale leave prompt
    for (const [id, value] of Object.entries({ username: 'admin', password: 'secret' })) {
      const input = document.querySelector<HTMLInputElement>(`#${id}`)!;
      input.value = value;
      input.dispatchEvent(new Event('input'));
    }
    findButton('Sign In')!.click();
    await vi.waitFor(() =>
      expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
    );
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  } finally {
    vi.useRealTimers();
  }
});

it('restores Settings URL and preserves history when two Back traversals are queued rapidly', async () => {
  window.history.replaceState(null, '', '/activity/history');
  const config = {
    wizardCompleted: true,
    port: 7171,
    aniListUsername: 'Viewer',
    aniListTvStatus: 'CURRENT',
    aniListMovieStatus: 'CURRENT',
    sonarrUrl: 'http://sonarr',
    sonarrApiKey: '******',
    sonarrQualityProfileId: 7,
    sonarrRootFolderPath: '/anime',
    sonarrMonitorOnAdd: true,
    sonarrSearchOnAdd: false,
    radarrUrl: 'http://radarr',
    radarrApiKey: '******',
    radarrQualityProfileId: 7,
    radarrRootFolderPath: '/anime',
    radarrMonitorOnAdd: true,
    radarrSearchOnAdd: false,
  };
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.endsWith('/auth/user')) return Response.json({ authenticated: true, wizardCompleted: true });
    if (url.endsWith('/config')) return Response.json(config);
    return Response.json([]);
  }));

  // Register before App mounts so we can queue a second Back right before App's restore go(+1)
  let queueSecondBack = false;
  const interleaveSecondBack = () => {
    if (queueSecondBack && window.location.pathname === '/library') {
      queueSecondBack = false;
      window.history.back();
    }
  };
  window.addEventListener('popstate', interleaveSecondBack);

  try {
    component = new App({ target: document.body });

    const findButton = (label: string) =>
      [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
    const findNavLink = (label: string) =>
      [...document.querySelectorAll('aside a')].find((a) => a.textContent?.trim() === label) as HTMLAnchorElement | undefined;

    await vi.waitFor(() => expect(findNavLink('Library')).toBeDefined());
    findNavLink('Library')!.click();
    await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
    findNavLink('Settings')!.click();
    await vi.waitFor(() =>
      expect([...document.querySelectorAll<HTMLInputElement>('input')].some((i) => i.value === '7171')).toBe(true),
    );

    const portInput = [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.value === '7171')!;
    portInput.value = '8099';
    portInput.dispatchEvent(new Event('input', { bubbles: true }));

    // Trigger Back to /library with a second Back to /activity/history queued ahead of App's restore
    queueSecondBack = true;
    window.history.back();
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
    await vi.waitFor(() => expect(window.location.pathname).toBe('/settings/general'));

    // Keep editing stays at Settings URL with edit intact
    findButton('Keep editing')!.click();
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
    expect(window.location.pathname).toBe('/settings/general');
    expect(portInput.value).toBe('8099');

    // Trigger the same interleaved double Back and choose Leave without saving -> lands on History
    queueSecondBack = true;
    window.history.back();
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
    await vi.waitFor(() => expect(window.location.pathname).toBe('/settings/general'));
    findButton('Leave without saving')!.click();
    await vi.waitFor(() => expect(window.location.pathname).toBe('/activity/history'));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  } finally {
    window.removeEventListener('popstate', interleaveSecondBack);
  }
});

it('completes navigation on Save and leave when an in-flight toolbar save finishes before confirmation', async () => {
  let resolveSave!: (res: Response) => void;
  let putCalls = 0;
  const { findButton, findNavLink, portInput } = await mountSettingsApp({
    onSave: () => {
      putCalls += 1;
      return new Promise<Response>((resolve) => {
        resolveSave = resolve;
      });
    },
  });

  const input = portInput();
  input.value = '7272';
  input.dispatchEvent(new Event('input', { bubbles: true }));

  // Start toolbar save, then open the leave prompt before it finishes
  await vi.waitFor(() =>
    expect(
      [...document.querySelectorAll('button')].find((b) => b.textContent?.includes('Save Changes'))?.disabled,
    ).toBe(false),
  );
  const saveChangesBtn = [...document.querySelectorAll('button')].find((b) =>
    b.textContent?.includes('Save Changes'),
  )!;
  saveChangesBtn.click();
  await vi.waitFor(() => expect(putCalls).toBe(1));

  findNavLink('Library')!.click();
  await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());

  // Toolbar save finishes cleanly while the modal is open
  resolveSave(Response.json({ status: 'ok' }));
  await vi.waitFor(() => expect(document.body.textContent).toContain('Configuration changes saved successfully.'));

  // Clicking Save and leave completes navigation without issuing another PUT
  findButton('Save and leave')!.click();
  await vi.waitFor(() => expect(window.location.pathname).toBe('/library'));
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(putCalls).toBe(1);
});
