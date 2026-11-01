import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { api, getBaseUrl, setCsrfToken, getCsrfToken, setApiKey, getApiKey, apiKeyStore } from './api';

describe('API Client', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    (globalThis as any).window = {};
    (globalThis as any).document = { cookie: '' };
    setCsrfToken('');
    setApiKey('');
  });

  it('manages API key storage and reactive store', () => {
    expect(getApiKey()).toBe('');
    expect(get(apiKeyStore)).toBe('');
    setApiKey('test-key-456');
    expect(getApiKey()).toBe('test-key-456');
    expect(get(apiKeyStore)).toBe('test-key-456');
  });

  it('resolves base URL with trailing slash handling', () => {
    expect(getBaseUrl()).toBe('');

    window.AniListArrSync = { urlBase: '/anilist-sync' };
    expect(getBaseUrl()).toBe('/anilist-sync');

    window.AniListArrSync = { urlBase: '/anilist-sync/' };
    expect(getBaseUrl()).toBe('/anilist-sync');
  });

  it('attaches CSRF token and API key to state-mutating requests', async () => {
    setCsrfToken('test-csrf-token');
    setApiKey('test-api-key');

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => ({ status: 'started' }),
    });
    globalThis.fetch = fetchMock;

    await api.startSync();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/sync/start');
    expect(options.method).toBe('POST');
    expect(options.headers['X-CSRF-Token']).toBe('test-csrf-token');
    expect(options.headers['X-Api-Key']).toBe('test-api-key');
  });

  it('does not require mutating headers for GET requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => ({ status: 'ok', version: '1.0.0' }),
    });
    globalThis.fetch = fetchMock;

    const health = await api.getHealth();
    expect(health.status).toBe('ok');

    const [, options] = fetchMock.mock.calls[0];
    expect(options.method).toBe('GET');
  });

  it('parses candidates JSON if stringified', async () => {
    const candidates = [
      { tvdbId: 101, tmdbId: 202, title: 'Test Anime', year: 2024, similarity: 0.95 },
    ];
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => [
        {
          id: 1,
          anilistId: 999,
          mediaType: 'SERIES',
          titleRomaji: 'Test Title',
          candidatesJson: JSON.stringify(candidates),
        },
      ],
    });
    globalThis.fetch = fetchMock;

    const items = await api.getReviewQueue();
    expect(items).toHaveLength(1);
    expect(items[0].candidates).toEqual(candidates);
  });

  it('extracts error messages from JSON error responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => ({ error: 'invalid override mapping' }),
    });
    globalThis.fetch = fetchMock;

    await expect(api.createOverride({ anilistId: 123, mediaType: 'SERIES' })).rejects.toThrow('invalid override mapping');
  });

  it('fetches and deletes ignored titles from blocklist', async () => {
    const ignoredData = [
      { anilistId: 123, titleRomaji: 'Ignored Show', reason: 'Dropped', createdAt: '2026-10-21T00:00:00Z' },
    ];
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ignoredData,
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ status: 'DELETED', anilistId: 123 }),
      });
    globalThis.fetch = fetchMock;

    const list = await api.getIgnored();
    expect(list).toEqual(ignoredData);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/ignored');
    expect(fetchMock.mock.calls[0][1].method).toBe('GET');

    const delRes = await api.deleteIgnored(123);
    expect(delRes.status).toBe('DELETED');
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/ignored/123');
    expect(fetchMock.mock.calls[1][1].method).toBe('DELETE');
  });

  it('fetches sync status and history correctly', async () => {
    const statusPayload = {
      running: false,
      lastReport: {
        runId: 10,
        durationMs: 1500,
        status: 'SUCCESS',
        itemsScanned: 42,
        addedRadarr: 2,
        monitoredSonarr: 5,
        unmonitoredSonarr: 0,
        unmonitoredRadarr: 0,
        queuedReview: 1,
        errors: [],
        trigger: 'MANUAL_WEB',
        startTime: '2026-10-31T01:00:00Z',
      },
    };
    const historyPayload = [
      {
        id: 1,
        runTimestamp: '2026-10-31T01:00:00Z',
        durationMs: 1500,
        status: 'SUCCESS',
        itemsScanned: 42,
        addedRadarr: 2,
        monitoredSonarr: 5,
        unmonitoredSonarr: 0,
        unmonitoredRadarr: 0,
        queuedReview: 1,
        triggerType: 'MANUAL_WEB',
      },
    ];

    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => statusPayload,
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => historyPayload,
      });
    globalThis.fetch = fetchMock;

    const status = await api.getSyncStatus();
    expect(status.isSyncing).toBe(false);
    expect(status.lastSync?.itemsScanned).toBe(42);
    expect(status.lastSync?.monitoredSonarr).toBe(5);

    const history = await api.getSyncHistory();
    expect(history).toHaveLength(1);
    expect(history[0].id).toBe(1);
    expect(history[0].itemsScanned).toBe(42);
  });

  it('fetches library items with query parameters and stats', async () => {
    const libraryData = [
      {
        anilistId: 101,
        mediaType: 'SERIES',
        format: 'TV',
        titleRomaji: 'Attack on Titan',
        anilistStatus: 'CURRENT',
        targetService: 'SONARR',
        monitoredStatus: 'MONITORED',
        lastSyncedAt: '2026-10-31T01:00:00Z',
        updatedAt: '2026-10-31T01:00:00Z',
      },
    ];
    const statsData = {
      total: 10,
      monitored: 8,
      unmonitored: 2,
      sonarrCount: 7,
      radarrCount: 3,
      watchingCount: 4,
      planningCount: 3,
      completedCount: 3,
    };

    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => libraryData,
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => statsData,
      });
    globalThis.fetch = fetchMock;

    const items = await api.getLibrary({ status: 'CURRENT', service: 'SONARR', search: 'Titan' });
    expect(items).toHaveLength(1);
    expect(items[0].anilistId).toBe(101);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/library?status=CURRENT&service=SONARR&search=Titan');

    const stats = await api.getLibraryStats();
    expect(stats.total).toBe(10);
    expect(stats.monitored).toBe(8);
  });
});
