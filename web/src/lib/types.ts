export interface Candidate {
  tvdbId?: number | null;
  tmdbId?: number | null;
  title: string;
  year?: number | null;
  similarity?: number | null;
}

export interface ReviewItem {
  id: number;
  mediaType: 'SERIES' | 'MOVIE';
  anilistId: number;
  titleRomaji: string;
  titleEnglish?: string | null;
  releaseYear?: number | null;
  status: string;
  reason: string;
  candidatesJson?: string | null;
  candidates?: Candidate[];
  posterUrl?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface OverrideItem {
  id: number;
  anilistId: number;
  mediaType: 'SERIES' | 'MOVIE';
  tvdbId?: number | null;
  tmdbId?: number | null;
  seasons: string;
  titleOverride?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface StagedAction {
  id: number;
  actionType: 'ADD_SERIES' | 'ADD_MOVIE' | 'MONITOR_SEASON' | 'UNMONITOR_SEASON' | 'UNMONITOR_MOVIE';
  mediaType: 'SERIES' | 'MOVIE';
  title: string;
  service: 'Sonarr' | 'Radarr';
  payload: string | Record<string, unknown>;
  status: 'PENDING' | 'APPLIED' | 'REJECTED';
  createdAt: string;
}

export interface SyncReport {
  startedAt: string;
  completedAt?: string;
  status: string;
  totalTitles: number;
  matchedTitles: number;
  unresolvedTitles: number;
  stagedActions: number;
  executionErrors?: string[];
}

export interface SyncStatus {
  isSyncing: boolean;
  lastSync?: SyncReport | null;
}

export interface SyncHistoryItem {
  id: number;
  startedAt: string;
  completedAt: string;
  status: 'SUCCESS' | 'PARTIAL' | 'FAILED' | 'RUNNING';
  triggerType: string;
  totalTitles: number;
  matchedTitles: number;
  unresolvedTitles: number;
  stagedActions: number;
  errorLog?: string | null;
}

export interface AppConfig {
  bindAddress: string;
  port: number;
  urlBase: string;
  enableSsl: boolean;
  sslPort: number;
  sslCertPath?: string;
  sslKeyPath?: string;
  authenticationMethod: 'None' | 'Forms' | 'External';
  authenticationRequired: 'Disabled' | 'Enabled' | 'DisabledForLocalAddresses';
  apiKey: string;
  logLevel: string;
  consoleLogLevel: string;
  instanceName: string;
  aniListUsername: string;
  syncIntervalHours: number;
  dateToleranceDays: number;
  confidenceThreshold: number;
  unmonitorDropped: boolean;
  firstRunDryRun: boolean;
  sonarrUrl: string;
  sonarrApiKey: string;
  radarrUrl: string;
  radarrApiKey: string;
  tagName: string;
}

export interface BackupItem {
  filename: string;
  size: number;
  createdAt: string;
}

export interface UpdateStatus {
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
  releaseNotes?: string;
  lastCheckedAt?: string;
}

export interface NotificationItem {
  id: number;
  name: string;
  type: 'DISCORD' | 'WEBHOOK';
  onSyncStart: boolean;
  onSyncSuccess: boolean;
  onSyncFailure: boolean;
  onReviewQueueNeeded: boolean;
  settings: Record<string, unknown>;
}

export interface CurrentUser {
  username: string;
  authenticated: boolean;
  csrfToken?: string;
}

export interface MappingResolution {
  mediaType: 'SERIES' | 'MOVIE';
  tvdbId?: number | null;
  tmdbId?: number | null;
  seasons?: string;
  titleOverride?: string;
}
