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
  episodeNumbers?: number[];
  titleOverride?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface StagedAction {
  id: number;
  actionType: 'ADD_SERIES' | 'ADD_MOVIE' | 'MONITOR_SEASON' | 'UNMONITOR_SEASON' | 'UNMONITOR_MOVIE' | 'DELETE_FILE' | 'UNMONITOR_EPISODE' | 'DELETE_EPISODE_FILE';
  mediaType: 'SERIES' | 'MOVIE';
  title: string;
  targetService?: string;
  service?: 'Sonarr' | 'Radarr' | string;
  payloadJson?: string;
  payload?: string | Record<string, unknown>;
  status: 'PENDING' | 'APPLIED' | 'REJECTED';
  createdAt: string;
}

export interface SyncReport {
  runId?: number;
  durationMs: number;
  status: 'SUCCESS' | 'PARTIAL' | 'FAILED';
  itemsScanned: number;
  addedRadarr: number;
  monitoredSonarr: number;
  unmonitoredSonarr: number;
  unmonitoredRadarr: number;
  queuedReview: number;
  errors?: string[];
  trigger: string;
  startTime?: string;
}

export interface SyncStatus {
  isSyncing: boolean;
  lastSync?: SyncReport | null;
}

export interface SyncHistoryItem {
  id: number;
  runTimestamp: string;
  durationMs: number;
  status: 'SUCCESS' | 'PARTIAL' | 'FAILED';
  itemsScanned: number;
  addedRadarr: number;
  monitoredSonarr: number;
  unmonitoredSonarr: number;
  unmonitoredRadarr: number;
  queuedReview: number;
  errorsJson?: string | null;
  triggerType: string;
}

export interface UpdateConfigResponse {
  status: string;
  message?: string;
  restartRequired?: boolean;
}

export interface AppConfig {
  bindAddress: string;
  port: number;
  urlBase: string;
  allowedHosts?: string;
  applicationUrl?: string;
  enableSsl: boolean;
  sslPort: number;
  sslCertPath?: string;
  sslKeyPath?: string;
  authenticationMethod: 'Forms' | 'Basic';
  authenticationRequired: 'Enabled' | 'DisabledForLocalAddresses';
  trustedNetworks?: string;
  apiKey: string;
  username?: string;
  password?: string;
  useProxy?: boolean;
  proxyType?: 'Http' | 'Socks4' | 'Socks5' | string;
  proxyHostname?: string;
  proxyPort?: number;
  proxyUsername?: string;
  proxyPassword?: string;
  proxyIgnoredAddresses?: string;
  proxyBypassLocalAddresses?: boolean;
  logLevel: string;
  consoleLogLevel?: string;
  logSizeLimit?: number;
  instanceName: string;
  wizardCompleted: boolean;
  restartRequired?: boolean;
  updateBranch?: string;
  updateAutomatically?: boolean;
  updateMechanism?: string;
  backupFolder?: string;
  backupInterval?: number;
  backupRetention?: number;
  aniListUsername: string;
  aniListTvStatus: string;
  aniListMovieStatus: string;
  syncIntervalHours: number;
  dateToleranceDays: number;
  confidenceThreshold: number;
  unmonitorDropped: boolean;
  unmonitorCompleted?: boolean;
  deleteDropped?: boolean;
  deleteCompleted?: boolean;
  unmonitorWatched?: boolean;
  deleteWatched?: boolean;
  firstRunDryRun: boolean;
  sonarrUrl: string;
  sonarrApiKey: string;
  sonarrQualityProfileId: number;
  sonarrRootFolderPath: string;
  sonarrMonitorOnAdd: boolean;
  sonarrSearchOnAdd: boolean;
  radarrUrl: string;
  radarrApiKey: string;
  radarrQualityProfileId: number;
  radarrRootFolderPath: string;
  radarrMonitorOnAdd: boolean;
  radarrSearchOnAdd: boolean;
  tagName: string;
}

export interface BackupItem {
  filename: string;
  size_bytes: number;
  created_at: string;
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
  provider?: string;
  configJson?: string;
  onAdded?: boolean;
  onReview?: boolean;
  onError?: boolean;
  onComplete?: boolean;
  onUpdate?: boolean;
  onSyncStart?: boolean;
  onSyncSuccess?: boolean;
  onSyncFailure?: boolean;
  onReviewQueueNeeded?: boolean;
  settings?: {
    webhookUrl?: string;
    url?: string;
    [key: string]: unknown;
  };
}

export interface CurrentUser {
  username: string;
  authenticated: boolean;
  csrfToken?: string;
  setupRequired?: boolean;
  wizardCompleted?: boolean;
  onboardingRequired?: boolean;
  authMethod?: 'Forms' | 'Basic' | string;
}

export interface AniListProfile {
  username: string;
  avatarUrl: string;
  animeCount: number;
  mangaCount: number;
}

export interface ServarrConnectionOptions {
  qualityProfiles: { id: number; name: string }[];
  rootFolders: { id: number; path: string }[];
}

export interface MappingResolution {
	preparationRequired?: boolean;
  mediaType: 'SERIES' | 'MOVIE';
  tvdbId?: number | null;
  tmdbId?: number | null;
  seasons?: string;
  episodeNumbers?: number[];
  titleOverride?: string;
}

export interface ReviewPreparation {
  status: 'WAITING' | 'READY' | 'ERROR' | 'CANCELLING' | 'CANCELLED' | 'RETAINED';
  message?: string;
  created: boolean;
  series?: { id: number; tvdbId: number; title: string; year?: number; seasons: { seasonNumber: number }[] };
  episodes?: { id: number; seasonNumber: number; episodeNumber: number; title: string; finaleType?: string }[];
}

export interface IgnoredItem {
  anilistId: number;
  titleRomaji: string;
  reason?: string;
  createdAt: string;
}

export interface LibraryItem {
  anilistId: number;
  mediaType: 'SERIES' | 'MOVIE';
  format?: string;
  titleRomaji: string;
  titleEnglish?: string | null;
  releaseYear?: number | null;
  posterUrl?: string | null;
  anilistStatus: string;
  targetService: 'SONARR' | 'RADARR' | 'NONE';
  tvdbId?: number | null;
  tmdbId?: number | null;
  arrTitle?: string | null;
  monitoredStatus: 'MONITORED' | 'UNMONITORED' | 'NOT_FOUND' | 'PENDING_REVIEW' | 'IGNORED';
  seasons?: string | null;
  lastSyncedAt: string;
  updatedAt: string;
}

export interface LibraryStats {
  total: number;
  monitored: number;
  unmonitored: number;
  sonarrCount: number;
  radarrCount: number;
  watchingCount: number;
  planningCount: number;
  completedCount: number;
}

export interface FilesystemDirectory {
  name: string;
  path: string;
}

export interface FilesystemResponse {
  current: string;
  parent?: string;
  roots: string[];
  directories: FilesystemDirectory[];
}
