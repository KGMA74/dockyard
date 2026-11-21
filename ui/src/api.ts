import type { components } from './generated/api'
import i18n from './i18n'

// Types generated from api/openapi.yaml (npm run gen-api — CI fails on
// drift). Hand-written interfaces below are being migrated progressively.
export type Role = NonNullable<components['schemas']['Role']>
export type GeneratedSchemas = components['schemas']

const BASE = '/api/admin'
const TOKEN_KEY = 'dockyard_token'
const REFRESH_KEY = 'dockyard_refresh'

function token(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

// Access tokens only live 15 minutes; a 401 triggers one silent refresh and a
// retry before giving up. The shared promise keeps concurrent 401s from
// spending the (single-use) refresh token more than once.
let refreshing: Promise<boolean> | null = null

function tryRefresh(): Promise<boolean> {
  refreshing ??= (async () => {
    const refreshToken = localStorage.getItem(REFRESH_KEY)
    if (!refreshToken) return false
    const res = await fetch(BASE + '/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })
    if (!res.ok) return false
    const data = (await res.json()) as { token: string; refresh_token: string }
    localStorage.setItem(TOKEN_KEY, data.token)
    localStorage.setItem(REFRESH_KEY, data.refresh_token)
    return true
  })()
    .catch(() => false)
    .finally(() => {
      refreshing = null
    })
  return refreshing
}

function clearSession(): void {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

/** Typed error carrying the HTTP status so callers (and react-query) can tell
 *  403/404 "feature off / not allowed" apart from a real 5xx outage. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly code?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function parseError(res: Response): Promise<ApiError> {
  const contentType = res.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) {
    const body = (await res.json().catch(() => ({}))) as { error?: string; code?: string }
    return new ApiError(res.status, body.error ?? `HTTP ${res.status}`, body.code)
  }
  return new ApiError(res.status, `HTTP ${res.status}`)
}

interface ReqOptions extends RequestInit {
  /** Skip the Authorization header and the 401-refresh dance (for /health). */
  noAuth?: boolean
}

async function req<T>(path: string, opts?: ReqOptions, retried = false): Promise<T> {
  const { noAuth, ...init } = opts ?? {}
  const res = await fetch(BASE + path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(noAuth ? {} : { Authorization: `Bearer ${token()}` }),
      ...opts?.headers,
    },
  })
  if (res.status === 401 && !noAuth) {
    if (!retried && (await tryRefresh())) {
      return req(path, opts, true)
    }
    clearSession()
    window.location.reload()
    throw new ApiError(401, 'Unauthorized')
  }
  if (res.status === 204) return undefined as T
  if (!res.ok) {
    throw await parseError(res)
  }
  return res.json() as Promise<T>
}

export type RegistryEventType = 'push' | 'delete' | 'retention' | 'gc' | 'scan' | 'import' | 'quota_warning'

export interface RegistryEvent {
  type: RegistryEventType
  name: string
  tag?: string
  actor?: string
}

// formatEventMessage turns a RegistryEvent into a short human sentence,
// shared between the push/completion toasts and the notification bell so
// the wording stays consistent.
export function formatEventMessage(event: RegistryEvent): string {
  const ref = event.name + (event.tag ? `:${event.tag}` : '')
  switch (event.type) {
    case 'push':
      return i18n.t('events.push', { ref })
    case 'delete':
      return i18n.t('events.delete', { ref })
    case 'retention':
      return i18n.t('events.retention', { ref })
    case 'gc':
      return event.actor === 'scheduler' ? i18n.t('events.gcScheduled') : i18n.t('events.gcManual')
    case 'scan':
      return i18n.t('events.scan', { ref })
    case 'import':
      return i18n.t('events.import', { name: event.name })
    case 'quota_warning':
      return i18n.t('events.quotaWarning', { name: event.name, threshold: event.tag ?? i18n.t('events.threshold') })
    default:
      return `${event.type}: ${ref}`
  }
}

/** True when the access token is missing, unreadable or about to expire. */
function accessTokenStale(): boolean {
  const exp = tokenClaims()?.exp
  return typeof exp !== 'number' || exp * 1000 - 30_000 < Date.now()
}

// Subscribes to the SSE feed of registry events. EventSource can't set an
// Authorization header, so the token travels as a query param instead.
//
// Reconnection is handled here rather than left to the browser: EventSource
// retries with the *same URL*, i.e. the same access token — which lives 15
// minutes. Left alone, the first dropped connection after that (proxy idle
// timeout, deploy) got a 401 and live updates silently stopped until a page
// reload. On error we close, refresh the token if needed, and reopen with
// exponential backoff. `onStatus` reports whether the feed is live.
function openEventStream(
  onEvent: (event: RegistryEvent) => void,
  onStatus?: (connected: boolean) => void,
): () => void {
  let source: EventSource | null = null
  let stopped = false
  let attempt = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  const open = () => {
    if (stopped || !isAuthenticated()) return
    source = new EventSource(`${BASE}/events?token=${encodeURIComponent(token())}`)
    source.onopen = () => {
      attempt = 0
      onStatus?.(true)
    }
    source.onmessage = e => {
      try {
        onEvent(JSON.parse(e.data) as RegistryEvent)
      } catch {
        // ignore malformed payloads
      }
    }
    source.onerror = () => {
      source?.close()
      onStatus?.(false)
      if (stopped) return
      const delay = Math.min(30_000, 1000 * 2 ** attempt++)
      timer = setTimeout(async () => {
        if (accessTokenStale()) await tryRefresh()
        open()
      }, delay)
    }
  }
  open()

  return () => {
    stopped = true
    clearTimeout(timer)
    source?.close()
  }
}

const EVENTS_LOCK = 'dockyard-events'
const EVENTS_CHANNEL = 'dockyard-events'

type FeedMessage =
  | { type: 'event'; event: RegistryEvent }
  | { type: 'status'; connected: boolean }
  | { type: 'hello' }

/** Subscribes to the registry's live event feed, with one connection per
 *  browser rather than per tab.
 *
 *  Every open console tab used to hold its own SSE stream. Over plain HTTP
 *  (HTTP/1.1) browsers allow about six connections per host, so from the
 *  sixth tab on every other request — page data, logins — queued behind the
 *  streams and the console looked frozen. Now the tab holding a Web Lock
 *  opens the only stream and relays what it receives over a
 *  BroadcastChannel; when it closes, the lock passes to another tab, which
 *  takes over. Without those APIs (old browsers), each tab streams on its
 *  own as before. */
export function subscribeToEvents(
  onEvent: (event: RegistryEvent) => void,
  onStatus?: (connected: boolean) => void,
): () => void {
  if (typeof BroadcastChannel === 'undefined' || !navigator.locks) {
    return openEventStream(onEvent, onStatus)
  }

  const channel = new BroadcastChannel(EVENTS_CHANNEL)
  const abort = new AbortController()
  let stopped = false
  let leading = false
  let lastStatus: boolean | undefined
  let stopStream: (() => void) | undefined
  let releaseLock: (() => void) | undefined

  const post = (message: FeedMessage) => {
    try {
      channel.postMessage(message)
    } catch {
      // channel closed while unsubscribing
    }
  }

  // A BroadcastChannel never delivers to the tab that posted, so the leader
  // handles its own events directly and only followers read these.
  channel.onmessage = ({ data }: MessageEvent<FeedMessage>) => {
    if (data.type === 'event') onEvent(data.event)
    else if (data.type === 'status') onStatus?.(data.connected)
    else if (data.type === 'hello' && leading && lastStatus !== undefined) {
      post({ type: 'status', connected: lastStatus })
    }
  }
  // A newly opened tab asks the leader, if any, whether the feed is live.
  post({ type: 'hello' })

  navigator.locks
    .request(EVENTS_LOCK, { signal: abort.signal }, () => {
      if (stopped) return Promise.resolve()
      leading = true
      stopStream = openEventStream(
        event => {
          onEvent(event)
          post({ type: 'event', event })
        },
        connected => {
          lastStatus = connected
          onStatus?.(connected)
          post({ type: 'status', connected })
        },
      )
      // Held until this tab unsubscribes (or closes, which frees it too).
      return new Promise<void>(resolve => {
        releaseLock = resolve
      })
    })
    .catch(() => {
      // AbortError: unsubscribed while still waiting for the lock.
    })

  return () => {
    stopped = true
    abort.abort()
    stopStream?.()
    releaseLock?.()
    channel.close()
  }
}

export function isAuthenticated(): boolean {
  return !!localStorage.getItem(TOKEN_KEY)
}

interface TokenClaims {
  sub?: string
  role?: string
  repos?: string[] | null
  src?: string
  exp?: number
}

function tokenClaims(): TokenClaims | null {
  const payload = token().split('.')[1]
  if (!payload) return null
  try {
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    return JSON.parse(json) as TokenClaims
  } catch {
    return null
  }
}

export function getUsername(): string | null {
  return tokenClaims()?.sub ?? null
}

// Default to least privilege when the token carries no role claim: a missing
// claim should never unlock admin-only UI. (The backend still enforces RBAC
// server-side regardless.)
export function getRole(): string {
  return tokenClaims()?.role ?? 'reader'
}

/** Repository patterns the signed-in account is limited to (empty = all). */
export function getRepoPatterns(): string[] {
  return tokenClaims()?.repos ?? []
}

export async function login(username: string, password: string): Promise<void> {
  const res = await fetch(BASE + '/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    // Typed, so the login form can tell "wrong password" (401) from "too
    // many attempts" (429) from "server unreachable" (5xx).
    throw await parseError(res)
  }
  const data = (await res.json()) as { token: string; refresh_token?: string }
  localStorage.setItem(TOKEN_KEY, data.token)
  if (data.refresh_token) localStorage.setItem(REFRESH_KEY, data.refresh_token)
}

/** Whether single sign-on is configured, and the provider's label. */
export async function getSSOInfo(): Promise<{ enabled: boolean; name?: string }> {
  const res = await fetch(BASE + '/auth/oidc')
  if (!res.ok) return { enabled: false }
  return (await res.json()) as { enabled: boolean; name?: string }
}

/** Where the browser goes to start an SSO sign-in. */
export function ssoLoginUrl(returnTo: string): string {
  return `${BASE}/auth/oidc/login?return=${encodeURIComponent(returnTo)}`
}

/** Finishes an SSO sign-in: trades the one-time code from the callback for
 *  the session tokens, stored like a password login's. */
export async function completeSSO(code: string): Promise<void> {
  const res = await fetch(BASE + '/auth/oidc/exchange', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  })
  if (!res.ok) throw await parseError(res)
  const data = (await res.json()) as { token: string; refresh_token?: string }
  localStorage.setItem(TOKEN_KEY, data.token)
  if (data.refresh_token) localStorage.setItem(REFRESH_KEY, data.refresh_token)
}

/** 'oidc' for single sign-on accounts, which have no password. */
export function getAuthSource(): string {
  return tokenClaims()?.src ?? 'local'
}

export async function getMe(): Promise<UserInfo> {
  return req('/auth/me')
}

export async function createCLISecret(): Promise<{ username: string; secret: string }> {
  return req('/auth/cli-secret', { method: 'POST' })
}

export async function deleteCLISecret(): Promise<void> {
  return req('/auth/cli-secret', { method: 'DELETE' })
}

export async function logout(): Promise<void> {
  await fetch(BASE + '/auth/logout', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token()}` },
  }).catch(() => {})
  clearSession()
}

export interface RepoSummary {
  name: string
  tags: string[]
  total: number
  last_pushed?: string
}

export async function getRepositories(): Promise<{ repositories: RepoSummary[]; total: number }> {
  return req('/repositories')
}

export interface TagScanSummary {
  id: number
  trivy_version?: string
  status: ScanResult['status']
  critical_count: number
  high_count: number
  medium_count: number
  low_count: number
  unknown_count: number
}

/** Optional fields are absent when unknown (no cosign keys, never scanned…). */
export interface TagInfo {
  tag: string
  digest: string
  pushed_at?: string
  size_bytes?: number
  platforms?: string[]
  signed?: boolean
  scan?: TagScanSummary
  /** The vulnerability policy refuses docker pull of this tag. */
  pull_blocked?: boolean
}

export async function getTags(name: string): Promise<{ name: string; tags: TagInfo[]; total: number }> {
  return req(`/repositories/tags?name=${encodeURIComponent(name)}`)
}

export async function deleteManifest(name: string, digest: string): Promise<void> {
  return req(`/repositories/manifests?name=${encodeURIComponent(name)}&digest=${encodeURIComponent(digest)}`, {
    method: 'DELETE',
  })
}

export async function deleteRepository(name: string): Promise<void> {
  return req(`/repositories?name=${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

export interface LayerInfo {
  digest: string
  size_bytes: number
  size_human: string
}

export interface PlatformInfo {
  architecture: string
  os: string
  variant?: string
  digest: string
  size_bytes: number
  size_human: string
}

export interface ManifestDetails {
  digest: string
  media_type: string
  total_size_bytes: number
  total_size_human: string
  layers: LayerInfo[]
  config_digest: string
  created?: string
  architecture?: string
  os?: string
  variant?: string
  platforms?: PlatformInfo[]
  signed?: boolean
  history?: HistoryStep[]
}

/** One build instruction; layer_digest/size_bytes only when it produced a layer. */
export interface HistoryStep {
  created_by: string
  created?: string
  empty_layer: boolean
  layer_digest?: string
  size_bytes?: number
}

export async function getManifestDetails(name: string, reference: string): Promise<ManifestDetails> {
  return req(`/repositories/manifest?name=${encodeURIComponent(name)}&reference=${encodeURIComponent(reference)}`)
}

export interface TagDiff {
  a: ManifestDetails
  b: ManifestDetails
  layers_only_a: string[]
  layers_only_b: string[]
  layers_common: string[]
  size_delta_bytes: number
}

export async function getTagDiff(name: string, referenceA: string, referenceB: string): Promise<TagDiff> {
  return req(`/repositories/diff?name=${encodeURIComponent(name)}&reference_a=${encodeURIComponent(referenceA)}&reference_b=${encodeURIComponent(referenceB)}`)
}

export interface SearchResult {
  name: string
  tag: string
  digest: string
  pushed_at?: string
  signed?: boolean
  scan?: { status: string; critical_count: number; high_count: number }
}

export async function searchRepositories(params: {
  q?: string
  signed?: boolean
  limit?: number
  offset?: number
} = {}): Promise<{ items: SearchResult[]; total: number; count: number }> {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.signed !== undefined) qs.set('signed', String(params.signed))
  if (params.limit) qs.set('limit', String(params.limit))
  if (params.offset) qs.set('offset', String(params.offset))
  const suffix = qs.toString()
  return req(`/repositories/search${suffix ? `?${suffix}` : ''}`)
}

export interface LayerEntry {
  path: string
  type: 'file' | 'dir' | 'symlink' | 'hardlink' | 'other'
  size?: number
  size_human?: string
  mode?: string
  link_name?: string
}

export async function getLayerEntries(name: string, digest: string): Promise<{ digest: string; entries: LayerEntry[]; count: number }> {
  return req(`/repositories/layer?name=${encodeURIComponent(name)}&digest=${encodeURIComponent(digest)}`)
}

/** `host[:port]` clients use to reach this registry — what goes in front of
 *  an image name in docker commands. */
export function registryHost(): string {
  return window.location.host
}

/** True when the page is served over plain HTTP from a non-loopback host:
 *  docker will then refuse to talk to it unless it is declared insecure. */
export function registryIsInsecure(): boolean {
  const { protocol, hostname } = window.location
  return protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(hostname)
}

export interface StorageStats {
  total_size_bytes: number
  total_size_human: string
  blob_count: number
  repo_count: number
  storage_path?: string
}

export async function getStorageStats(): Promise<StorageStats> {
  try {
    return await req<StorageStats>('/storage/stats')
  } catch (err) {
    // proxy mode: stats not supported → return sentinel values
    if (err instanceof ApiError && err.status === 501) {
      return { total_size_bytes: -1, total_size_human: '—', blob_count: -1, repo_count: -1 }
    }
    throw err
  }
}

export interface GCResult {
  count: number
  freed_human: string
  freed_bytes: number
  removed: string[]
  dry_run?: boolean
  /** Total blobs enumerated. 0 while repositories exist means the backend
   *  could not list storage (e.g. missing S3 ListBucket permission). */
  scanned_blobs?: number
  /** Unreferenced blobs left alone because they were written within the
   *  grace period (they may belong to a push that is still in flight). */
  skipped_recent?: number
  /** Unreferenced blobs the backend refused to delete. */
  failed?: number
  grace_period_seconds?: number
}

export async function runGC(options: { dryRun?: boolean; force?: boolean } = {}): Promise<GCResult> {
  const qs = new URLSearchParams()
  if (options.dryRun) qs.set('dryRun', 'true')
  if (options.force) qs.set('force', 'true')
  const suffix = qs.toString()
  return req(`/gc${suffix ? `?${suffix}` : ''}`, { method: 'POST' })
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return req('/auth/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

export interface UserInfo {
  id: number
  username: string
  role: Role
  repo_patterns: string[]
  created_at: string
  updated_at: string
  /** "oidc": signs in through SSO and has no password. */
  auth_source: 'local' | 'oidc'
  /** Has a CLI secret for docker login. */
  has_cli_secret: boolean
}

export async function listUsers(): Promise<{ users: UserInfo[]; count: number }> {
  return req('/users')
}

export async function createUser(username: string, password: string, role: string, repoPatterns: string[]): Promise<UserInfo> {
  return req('/users', {
    method: 'POST',
    body: JSON.stringify({ username, password, role, repo_patterns: repoPatterns }),
  })
}

export async function updateUser(
  username: string,
  changes: { role?: string; repo_patterns?: string[]; password?: string },
): Promise<UserInfo> {
  return req(`/users/${encodeURIComponent(username)}`, {
    method: 'PUT',
    body: JSON.stringify(changes),
  })
}

export async function deleteUser(username: string): Promise<void> {
  return req(`/users/${encodeURIComponent(username)}`, { method: 'DELETE' })
}

export type RobotRole = 'pusher' | 'reader'

/** A registry-only credential for CI pipelines and clusters. */
export interface RobotInfo {
  id: number
  name: string
  /** What `docker login -u` takes: robot+<name>. */
  username: string
  description: string
  role: RobotRole
  repo_patterns: string[]
  created_by: string
  created_at: string
  expires_at: string | null
  last_used_at: string | null
}

/** Returned on creation and rotation only — the secret is never shown again. */
export interface RobotWithSecret extends RobotInfo {
  secret: string
}

export async function listRobots(): Promise<{ robots: RobotInfo[]; count: number }> {
  return req('/robots')
}

export async function createRobot(robot: {
  name: string
  description: string
  role: RobotRole
  repo_patterns: string[]
  expires_in_days: number
}): Promise<RobotWithSecret> {
  return req('/robots', { method: 'POST', body: JSON.stringify(robot) })
}

export async function rotateRobotSecret(id: number, expiresInDays: number): Promise<RobotWithSecret> {
  return req(`/robots/${id}/secret`, { method: 'POST', body: JSON.stringify({ expires_in_days: expiresInDays }) })
}

export async function deleteRobot(id: number): Promise<void> {
  return req(`/robots/${id}`, { method: 'DELETE' })
}

export interface SessionInfo {
  id: number
  username: string
  user_agent: string
  ip: string
  created_at: string
  last_seen_at: string
  expires_at: string
}

export async function listSessions(): Promise<{ sessions: SessionInfo[]; count: number; current_id: number }> {
  return req('/sessions')
}

export async function revokeSession(id: number): Promise<void> {
  return req(`/sessions/${id}`, { method: 'DELETE' })
}

/** The caller's own sessions — any signed-in user. */
export async function listMySessions(): Promise<{ sessions: SessionInfo[]; count: number; current_id: number }> {
  return req('/sessions/mine')
}

export async function revokeMySession(id: number): Promise<void> {
  return req(`/sessions/mine/${id}`, { method: 'DELETE' })
}

export type ConfigGroupId = 'registry' | 'access' | 'security' | 'maintenance' | 'observability'

export interface ConfigItem {
  key: string
  value: boolean | number | string | string[]
  env?: string
}

/** Effective server configuration, secrets left out (admin only). */
export type SetupStepId = 'admin_password' | 'first_image' | 'robot_account' | 'scan_on_push' | 'team' | 'https'

/** The getting-started checklist, ticked from the instance's state (admin). */
export async function getSetup(): Promise<{ steps: { id: SetupStepId; done: boolean }[] }> {
  return req('/setup')
}

export async function getServerConfig(): Promise<{ groups: { id: ConfigGroupId; items: ConfigItem[] }[] }> {
  return req('/config')
}

export interface AuditEntry {
  id: number
  at: string
  actor: string
  action: string
  repo?: string
  tag?: string
  source_ip?: string
  result: string
  details?: string
}

export async function getAudit(
  params: { limit?: number; offset?: number; actor?: string; repo?: string } = {},
): Promise<{ entries: AuditEntry[]; total: number }> {
  const qs = new URLSearchParams()
  qs.set('limit', String(params.limit ?? 50))
  if (params.offset) qs.set('offset', String(params.offset))
  if (params.actor) qs.set('actor', params.actor)
  if (params.repo) qs.set('repo', params.repo)
  return req(`/audit?${qs.toString()}`)
}

export interface RetentionPolicy {
  id: number
  repo_pattern: string
  keep_n: number
  unpulled_days: number
  keep_patterns: string[]
  protected_tags: string[]
  enabled: boolean
  created_at: string
}

export interface RetentionPlan {
  delete: { repo: string; tag: string; digest: string; reason: string }[]
  skipped: { repo: string; tag: string; reason: string }[]
}

export async function listRetentionPolicies(): Promise<{ policies: RetentionPolicy[]; count: number }> {
  return req('/retention')
}

export async function createRetentionPolicy(policy: {
  repo_pattern: string
  keep_n: number
  unpulled_days: number
  keep_patterns: string[]
  protected_tags: string[]
}): Promise<RetentionPolicy> {
  return req('/retention', { method: 'POST', body: JSON.stringify(policy) })
}

export async function deleteRetentionPolicy(id: number): Promise<void> {
  return req(`/retention/${id}`, { method: 'DELETE' })
}

export async function runRetention(dryRun: boolean): Promise<{ plan: RetentionPlan; dry_run: boolean; deleted: number }> {
  return req(`/retention/run${dryRun ? '?dryRun=true' : ''}`, { method: 'POST' })
}

export interface WebhookInfo {
  id: number
  url: string
  events: string[]
  format: 'generic' | 'slack' | 'discord'
  enabled: boolean
  created_at: string
}

export async function listWebhooks(): Promise<{ webhooks: WebhookInfo[]; count: number }> {
  return req('/webhooks')
}

export async function createWebhook(hook: {
  url: string
  secret: string
  events: string[]
  format: string
}): Promise<WebhookInfo> {
  return req('/webhooks', { method: 'POST', body: JSON.stringify(hook) })
}

export async function deleteWebhook(id: number): Promise<void> {
  return req(`/webhooks/${id}`, { method: 'DELETE' })
}

export async function testWebhook(id: number): Promise<void> {
  return req(`/webhooks/${id}/test`, { method: 'POST' })
}

export interface ScanResult {
  id: number
  name: string
  reference: string
  digest: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  requested_by: string
  trivy_version?: string
  critical_count: number
  high_count: number
  medium_count: number
  low_count: number
  unknown_count: number
  error?: string
  started_at?: string
  finished_at?: string
  created_at: string
}

export async function listScans(params: { name?: string; digest?: string; limit?: number; offset?: number } = {}): Promise<{ scans: ScanResult[]; count: number }> {
  const qs = new URLSearchParams()
  if (params.name) qs.set('name', params.name)
  if (params.digest) qs.set('digest', params.digest)
  if (params.limit) qs.set('limit', String(params.limit))
  if (params.offset) qs.set('offset', String(params.offset))
  const suffix = qs.toString()
  return req(`/scans${suffix ? `?${suffix}` : ''}`)
}

export async function triggerScan(name: string, reference: string): Promise<{ scan: ScanResult; cached: boolean }> {
  return req('/scans', { method: 'POST', body: JSON.stringify({ name, reference }) })
}

export async function getScan(id: number): Promise<ScanResult> {
  return req(`/scans/${id}`)
}

export type Severity = 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW' | 'UNKNOWN'

export interface Vulnerability {
  id: string
  pkg: string
  installed: string
  fixed: string
  severity: Severity
  title: string
  url: string
  target: string
}

interface TrivyReport {
  Results?: {
    Target?: string
    Vulnerabilities?: {
      VulnerabilityID?: string
      PkgName?: string
      InstalledVersion?: string
      FixedVersion?: string
      Severity?: string
      Title?: string
      PrimaryURL?: string
    }[]
  }[]
}

const SEVERITY_RANK: Record<Severity, number> = { CRITICAL: 0, HIGH: 1, MEDIUM: 2, LOW: 3, UNKNOWN: 4 }

/** Flattens a raw Trivy JSON report into rows, most severe first. */
export function parseTrivyReport(report: unknown): Vulnerability[] {
  const rows: Vulnerability[] = []
  for (const result of (report as TrivyReport)?.Results ?? []) {
    for (const v of result.Vulnerabilities ?? []) {
      const severity = (v.Severity ?? '').toUpperCase()
      rows.push({
        id: v.VulnerabilityID ?? '—',
        pkg: v.PkgName ?? '—',
        installed: v.InstalledVersion ?? '',
        fixed: v.FixedVersion ?? '',
        severity: severity in SEVERITY_RANK ? (severity as Severity) : 'UNKNOWN',
        title: v.Title ?? '',
        url: v.PrimaryURL ?? '',
        target: result.Target ?? '',
      })
    }
  }
  return rows.sort(
    (a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || a.pkg.localeCompare(b.pkg),
  )
}

export async function getScanVulnerabilities(id: number): Promise<Vulnerability[]> {
  return parseTrivyReport(await req<unknown>(`/scans/${id}/report`))
}

export type ReportFormat = 'json' | 'csv' | 'pdf'

/** Downloads a scan report as a file. The endpoint needs the bearer token, so
 *  a plain link would not do: fetch it, then hand the browser a blob URL. */
export async function downloadScanReport(id: number, format: ReportFormat, retried = false): Promise<void> {
  const lang = i18n.language?.startsWith('fr') ? 'fr' : 'en'
  const res = await fetch(`${BASE}/scans/${id}/report?format=${format}&lang=${lang}`, {
    headers: { Authorization: `Bearer ${token()}` },
  })
  if (res.status === 401 && !retried && (await tryRefresh())) {
    return downloadScanReport(id, format, true)
  }
  if (!res.ok) throw await parseError(res)
  const name = /filename="([^"]+)"/.exec(res.headers.get('content-disposition') ?? '')?.[1] ?? `scan-${id}.${format}`
  const url = URL.createObjectURL(await res.blob())
  try {
    const a = document.createElement('a')
    a.href = url
    a.download = name
    document.body.appendChild(a)
    a.click()
    a.remove()
  } finally {
    // Revoked on the next tick: the click has started the download by then.
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}

export interface SigningPolicy {
  id: number
  repo_pattern: string
  required: boolean
  created_at: string
}

export async function getSigningStatus(): Promise<{ enabled: boolean; keys_loaded: number }> {
  return req('/signing')
}

export async function listSigningPolicies(): Promise<{ policies: SigningPolicy[]; count: number }> {
  return req('/signing/policies')
}

export async function createSigningPolicy(repoPattern: string, required: boolean): Promise<SigningPolicy> {
  return req('/signing/policies', { method: 'POST', body: JSON.stringify({ repo_pattern: repoPattern, required }) })
}

export async function deleteSigningPolicy(id: number): Promise<void> {
  return req(`/signing/policies/${id}`, { method: 'DELETE' })
}

/** '' = never block; otherwise the lowest severity that blocks a pull. */
export type BlockSeverity = '' | 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW'

export interface VulnerabilityPolicy {
  scan_on_push: boolean
  block_severity: BlockSeverity
  updated_by: string
  updated_at: string
}

export interface AllowedCVE {
  cve_id: string
  reason: string
  expires_at: string | null
  created_by: string
  created_at: string
}

export async function getVulnerabilityPolicy(): Promise<{
  policy: VulnerabilityPolicy
  allowlist: AllowedCVE[]
  scanning_enabled: boolean
}> {
  return req('/vulnerability-policy')
}

export async function updateVulnerabilityPolicy(policy: { scan_on_push: boolean; block_severity: BlockSeverity }): Promise<VulnerabilityPolicy> {
  return req('/vulnerability-policy', { method: 'PUT', body: JSON.stringify(policy) })
}

export type SSOSettings = GeneratedSchemas['SSOSettings']
export type SSOSettingsInput = GeneratedSchemas['SSOSettingsInput']
export type SSOTestResult = GeneratedSchemas['SSOTestResult']

/** Single sign-on configuration (admin). The client secret never comes back. */
export async function getSSOSettings(): Promise<SSOSettings> {
  return req('/sso')
}

/** Saves and applies at once. Omit client_secret to keep the stored one. */
export async function updateSSOSettings(settings: SSOSettingsInput): Promise<SSOSettings> {
  return req('/sso', { method: 'PUT', body: JSON.stringify(settings) })
}

/** OpenID discovery against an issuer, to catch a typo before saving. */
export async function testSSOIssuer(issuerUrl: string): Promise<SSOTestResult> {
  return req('/sso/test', { method: 'POST', body: JSON.stringify({ issuer_url: issuerUrl }) })
}

export async function allowCVE(entry: { cve_id: string; reason: string; expires_in_days: number }): Promise<void> {
  return req('/vulnerability-policy/allowlist', { method: 'POST', body: JSON.stringify(entry) })
}

export async function disallowCVE(cveId: string): Promise<void> {
  return req(`/vulnerability-policy/allowlist/${encodeURIComponent(cveId)}`, { method: 'DELETE' })
}

export type QuotaScopeType = 'repo' | 'user'

export interface Quota {
  id: number
  scope_type: QuotaScopeType
  scope_value: string
  max_bytes: number
  warn_percent: number
  created_at: string
}

export interface QuotaUsage {
  scope_type: QuotaScopeType
  scope_value: string
  bytes_used: number
}

export async function listQuotas(): Promise<{ quotas: Quota[]; usage: QuotaUsage[] }> {
  return req('/quotas')
}

export async function setQuota(scopeType: QuotaScopeType, scopeValue: string, maxBytes: number, warnPercent: number): Promise<Quota> {
  return req('/quotas', {
    method: 'PUT',
    body: JSON.stringify({ scope_type: scopeType, scope_value: scopeValue, max_bytes: maxBytes, warn_percent: warnPercent }),
  })
}

export async function deleteQuota(id: number): Promise<void> {
  return req(`/quotas/${id}`, { method: 'DELETE' })
}

export async function resetQuotaUsage(scopeType: QuotaScopeType, scopeValue: string): Promise<void> {
  return req('/quotas/usage/reset', { method: 'POST', body: JSON.stringify({ scope_type: scopeType, scope_value: scopeValue }) })
}

export interface ReplicationTarget {
  id: number
  name: string
  base_url: string
  username: string
  repo_pattern: string
  enabled: boolean
  created_at: string
}

export async function listReplicationTargets(): Promise<{ targets: ReplicationTarget[]; count: number }> {
  return req('/replication/targets')
}

export async function createReplicationTarget(
  name: string, baseUrl: string, username: string, password: string, repoPattern: string,
): Promise<ReplicationTarget> {
  return req('/replication/targets', {
    method: 'POST',
    body: JSON.stringify({ name, base_url: baseUrl, username, password, repo_pattern: repoPattern }),
  })
}

export async function deleteReplicationTarget(id: number): Promise<void> {
  return req(`/replication/targets/${id}`, { method: 'DELETE' })
}

export async function testReplicationTarget(id: number): Promise<void> {
  return req(`/replication/targets/${id}/test`, { method: 'POST' })
}

export interface StatsSample {
  at: string
  total_size: number
  blob_count: number
  repo_count: number
}

export interface RepoSize {
  name: string
  size_bytes: number
  size_human: string
  tags: number
}

export async function getInsights(): Promise<{ history: StatsSample[]; top_repos: RepoSize[] }> {
  return req('/insights')
}

export interface HealthInfo {
  status: string
  mode: 'embedded' | 'proxy' | 'mirror'
  registry?: string
  version?: string
  mirror?: { hits: number; misses: number }
  storage?: {
    ok: boolean
    latency_ms: number
    error?: string
    blobs?: number
    bytes?: number
    repositories?: number
    free_bytes?: number
  }
}

export async function getHealth(): Promise<HealthInfo> {
  const res = await fetch('/health')
  if (!res.ok) throw new ApiError(res.status, `HTTP ${res.status}`)
  return res.json()
}
