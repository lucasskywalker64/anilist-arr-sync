// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import LifecycleActions from './LifecycleActions.svelte';
import { api, waitForRestart } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  api: {
    getSystemStatus: vi.fn().mockResolvedValue({ isContainerized: false }),
    restart: vi.fn(),
  },
  waitForRestart: vi.fn(),
}));

let component: LifecycleActions | undefined;
afterEach(() => {
  component?.$destroy();
  component = undefined;
  document.body.replaceChildren();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it('navigates to HTTP only after the HTTPS handoff reports readiness', async () => {
  const assign = vi.fn();
  const browserWindow = window;
  vi.stubGlobal('window', {
    location: { protocol: 'https:', origin: 'https://remote.example', assign },
    addEventListener: browserWindow.addEventListener.bind(browserWindow),
    removeEventListener: browserWindow.removeEventListener.bind(browserWindow),
  });
  const reconnectUrl = 'http://remote.example:7171/';
  const statusUrl = '/api/v1/system/restart-status/operation';
  vi.mocked(api.restart).mockResolvedValue({ restarting: true, instanceId: 'old', reconnectUrl, statusUrl });
  let ready!: () => void;
  vi.mocked(waitForRestart).mockReturnValue(new Promise<void>(resolve => { ready = resolve; }));
  component = new LifecycleActions({ target: document.body });
  document.querySelector<HTMLButtonElement>('.menu button')!.click();
  await tick();
  const confirm = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')]
    .find(button => button.textContent?.trim() === 'Restart');
  expect(confirm).toBeDefined();
  confirm!.click();
  await vi.waitFor(() => expect(waitForRestart).toHaveBeenCalledWith('old', reconnectUrl, expect.any(AbortSignal), statusUrl));
  expect(assign).not.toHaveBeenCalled();
  ready();
  await vi.waitFor(() => expect(assign).toHaveBeenCalledWith(reconnectUrl));
});
