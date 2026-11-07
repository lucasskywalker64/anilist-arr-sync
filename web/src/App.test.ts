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
