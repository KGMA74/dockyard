import type { components } from './generated/api'

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

async function req<T>(path: string, opts?: RequestInit, retried = false): Promise<T> {
  const res = await fetch(BASE + path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token()}`,
      ...opts?.headers,
    },
  })
  if (res.status === 401) {
    if (!retried && (await tryRefresh())) {
      return req(path, opts, true)
    }
    clearSession()
    window.location.reload()
    throw new Error('Unauthorized')
  }
  if (res.status === 204) return undefined as T
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? `HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export interface PushEvent {
  type: 'push'
  name: string
  tag?: string
}

// Subscribes to the SSE push feed. EventSource can't set an Authorization
// header, so the token travels as a query param instead — the browser
// reconnects automatically on drop, no manual retry logic needed here.
export function subscribeToPushEvents(onPush: (event: PushEvent) => void): () => void {
  const es = new EventSource(`${BASE}/events?token=${encodeURIComponent(token())}`)
  es.onmessage = e => {
    try {
      const data = JSON.parse(e.data) as PushEvent
      if (data.type === 'push') onPush(data)
    } catch {
      // ignore malformed payloads
    }
  }
  return () => es.close()
}

export function isAuthenticated(): boolean {
  return !!localStorage.getItem(TOKEN_KEY)
}

function tokenClaims(): { sub?: string; role?: string } | null {
  const payload = token().split('.')[1]
  if (!payload) return null
  try {
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    return JSON.parse(json) as { sub?: string; role?: string }
  } catch {
    return null
  }
}

export function getUsername(): string | null {
  return tokenClaims()?.sub ?? null
}

// Tokens issued before the RBAC release have no role claim — the only account
// back then was the admin, mirror the backend's fallback.
export function getRole(): string {
  return tokenClaims()?.role ?? 'admin'
}

export async function login(username: string, password: string): Promise<void> {
  const res = await fetch(BASE + '/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error((body as { error?: string }).error ?? 'Login failed')
  }
  const data = (await res.json()) as { token: string; refresh_token?: string }
  localStorage.setItem(TOKEN_KEY, data.token)
  if (data.refresh_token) localStorage.setItem(REFRESH_KEY, data.refresh_token)
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

export interface TagInfo {
  tag: string
  digest: string
  pushed_at?: string
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
  platforms?: PlatformInfo[]
  signed?: boolean
}

export async function getManifestDetails(name: string, reference: string): Promise<ManifestDetails> {
  return req(`/repositories/manifest?name=${encodeURIComponent(name)}&reference=${encodeURIComponent(reference)}`)
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

export interface StorageStats {
  total_size_bytes: number
  total_size_human: string
  blob_count: number
  repo_count: number
  storage_path?: string
}

export async function getStorageStats(): Promise<StorageStats> {
  const res = await fetch(BASE + '/storage/stats', {
    headers: { Authorization: `Bearer ${token()}` },
  })
  // proxy mode: stats not supported → return sentinel values
  if (res.status === 501) {
    return { total_size_bytes: -1, total_size_human: '—', blob_count: -1, repo_count: -1 }
  }
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

export interface GCResult {
  count: number
  freed_human: string
  freed_bytes: number
  removed: string[]
  dry_run?: boolean
}

export async function runGC(dryRun = false): Promise<GCResult> {
  return req(`/gc${dryRun ? '?dryRun=true' : ''}`, { method: 'POST' })
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

export async function getAudit(limit = 50): Promise<{ entries: AuditEntry[]; total: number }> {
  return req(`/audit?limit=${limit}`)
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
}

export async function getHealth(): Promise<HealthInfo> {
  const res = await fetch('/health')
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}
