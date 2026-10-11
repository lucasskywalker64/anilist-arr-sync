export interface SyncReport {
  trigger: string;
  startTime: string;
  endTime: string;
  durationMs: number;
  itemsScanned: number;
  addedRadarr: number;
  monitoredSonarr: number;
  unmonitoredSonarr: number;
  unmonitoredRadarr: number;
  queuedReview: number;
  errors?: string[];
}

export interface SyncStatus {
  running: boolean;
  lastReport: SyncReport | null;
}

export interface SyncHistoryRecord {
  id: number;
  runTimestamp: string;
  durationMs: number;
  status: string;
  itemsScanned: number;
  addedRadarr: number;
  monitoredSonarr: number;
  unmonitoredSonarr: number;
  unmonitoredRadarr: number;
  queuedReview: number;
  errorsJson?: string | null;
  triggerType: string;
}

export interface StagedAction {
  id: number;
  runId?: number | null;
  actionType: 'ADD_MOVIE' | 'ADD_SERIES' | 'MONITOR_SEASON' | 'UNMONITOR_SEASON' | 'UNMONITOR_MOVIE' | string;
  mediaType: string;
  title: string;
  targetService: string;
  payloadJson: string;
  status: 'PENDING' | 'APPLIED' | 'REJECTED' | string;
  createdAt: string;
  executedAt?: string | null;
}

export interface CandidateInfo {
  id: number;
  title: string;
  year?: number;
  overview?: string;
  score?: number;
}

export interface ReviewQueueItem {
  id: number;
  anilistId: number;
  mediaType: 'MOVIE' | 'SERIES' | string;
  titleRomaji: string;
  titleEnglish?: string | null;
  releaseYear?: number | null;
  posterUrl?: string | null;
  candidatesJson: string;
  reason: string;
  status: 'PENDING' | 'RESOLVED' | 'IGNORED' | string;
  resolvedId?: number | null;
  resolvedSeason?: number | null;
  createdAt: string;
  resolvedAt?: string | null;
}

export interface MappingOverride {
  anilistId: number;
  mediaType: 'MOVIE' | 'SERIES' | string;
  tvdbId?: number | null;
  tmdbId?: number | null;
  seasons: string;
  titleOverride?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface AppConfig {
  bindAddress?: string;
  port?: number;
  urlBase?: string;
  enableSsl?: boolean;
  sslPort?: number;
  authenticationMethod?: string;
  authenticationRequired?: string;
  apiKey?: string;
  logLevel?: string;
  consoleLogLevel?: string;
  instanceName?: string;
  databasePath?: string;
  aniListUsername?: string;
  radarrUrl?: string;
  sonarrUrl?: string;
  tagName?: string;
  syncIntervalHours?: number;
  dateToleranceDays?: number;
  confidenceThreshold?: number;
  unmonitorDropped?: boolean;
  firstRunDryRun?: boolean;
}

export interface UserInfo {
  username: string;
  authMethod: string;
}

export interface ResolveReviewInput {
  mediaType?: string;
  tvdbId?: number;
  tmdbId?: number;
  seasons?: string;
  titleOverride?: string;
}

export interface CreateOverrideInput {
  anilistId: number;
  mediaType: string;
  tvdbId?: number;
  tmdbId?: number;
  seasons?: string;
  titleOverride?: string;
}

let apiKey = '';
if (typeof localStorage !== 'undefined') {
  apiKey = localStorage.getItem('anilist_arr_sync_apikey') || '';
}

export function getApiKey(): string {
  return apiKey;
}

export function setApiKey(newKey: string): void {
  apiKey = newKey;
  if (typeof localStorage !== 'undefined') {
    if (newKey) {
      localStorage.setItem('anilist_arr_sync_apikey', newKey);
    } else {
      localStorage.removeItem('anilist_arr_sync_apikey');
    }
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const method = (options.method || 'GET').toUpperCase();
  const isMutating = ['POST', 'PUT', 'DELETE', 'PATCH'].includes(method);

  const headers: Record<string, string> = {
    'Accept': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };

  if (options.body && typeof options.body === 'string' && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }

  // Attach X-Api-Key if configured
  if (apiKey) {
    headers['X-Api-Key'] = apiKey;
  }

  const response = await fetch(path, {
    ...options,
    headers,
  });

  if (!response.ok) {
    let errorMsg = `HTTP ${response.status}: ${response.statusText}`;
    try {
      const data = await response.json();
      if (data && data.error) {
        errorMsg = data.error;
      }
    } catch {
      // Ignore JSON parse errors for non-json response bodies
    }
    const err = new Error(errorMsg);
    (err as any).status = response.status;
    throw err;
  }

  const contentType = response.headers && typeof response.headers.get === 'function'
    ? (response.headers.get('Content-Type') || '')
    : 'application/json';
  if (contentType.includes('application/json')) {
    return (await response.json()) as T;
  }
  return null as unknown as T;
}

export const api = {
  // Health
  getHealth: () => request<{ status: string; version: string }>('/api/v1/health'),

  // Auth
  getCurrentUser: () => request<UserInfo>('/api/v1/auth/user'),
  login: (username: string, password: string) =>
    request<{ status: string; user: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () =>
    request<{ status: string }>('/api/v1/auth/logout', {
      method: 'POST',
    }),
  setupAdmin: (username: string, password: string) =>
    request<{ status: string; user: string }>('/api/v1/auth/setup', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  // Sync operations
  getSyncStatus: () => request<SyncStatus>('/api/v1/sync/status'),
  getSyncHistory: () => request<SyncHistoryRecord[]>('/api/v1/sync/history'),
  triggerSync: () =>
    request<{ status: string; trigger: string }>('/api/v1/sync/start', {
      method: 'POST',
    }),

  // Staged dry-run actions
  getStagedActions: () => request<StagedAction[]>('/api/v1/staged'),
  applyStaged: (ids?: number[]) =>
    request<{ status: string; appliedCount: number }>('/api/v1/staged/apply', {
      method: 'POST',
      body: JSON.stringify(ids && ids.length > 0 ? { ids } : {}),
    }),
  rejectStaged: (ids?: number[]) =>
    request<{ status: string; rejectedCount: number }>('/api/v1/staged/reject', {
      method: 'POST',
      body: JSON.stringify(ids && ids.length > 0 ? { ids } : {}),
    }),

  // Review Queue
  getReviewQueue: () => request<ReviewQueueItem[]>('/api/v1/review'),
  resolveReview: (id: number, data: ResolveReviewInput) =>
    request<{ status: string; id: number; anilistId: number }>(`/api/v1/review/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  ignoreReview: (id: number, reason?: string) =>
    request<{ status: string; id: number; anilistId: number }>(`/api/v1/review/${id}/ignore`, {
      method: 'POST',
      body: JSON.stringify({ reason: reason || 'Ignored from review queue' }),
    }),

  // Mapping Overrides
  getOverrides: () => request<MappingOverride[]>('/api/v1/overrides'),
  createOverride: (data: CreateOverrideInput) =>
    request<{ status: string; anilistId: number }>('/api/v1/overrides', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  deleteOverride: (anilistId: number) =>
    request<{ status: string; anilistId: number }>(`/api/v1/overrides/${anilistId}`, {
      method: 'DELETE',
    }),

  // Configuration
  getConfig: () => request<AppConfig>('/api/v1/config'),
  updateConfig: (updates: Partial<AppConfig>) =>
    request<{ status: string; message: string }>('/api/v1/config', {
      method: 'PUT',
      body: JSON.stringify(updates),
    }),
};
