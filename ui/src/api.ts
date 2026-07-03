const BASE = '/api/admin'
const TOKEN_KEY = 'dockyard_token'

function token(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

async function req<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token()}`,
      ...opts?.headers,
    },
  })
  if (res.status === 401) {
    localStorage.removeItem(TOKEN_KEY)
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

export function getUsername(): string | null {
  const raw = token()
  const payload = raw.split('.')[1]
  if (!payload) return null
  try {
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    return (JSON.parse(json) as { sub?: string }).sub ?? null
  } catch {
    return null
  }
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
  const data = (await res.json()) as { token: string }
  localStorage.setItem(TOKEN_KEY, data.token)
}

export async function logout(): Promise<void> {
  await fetch(BASE + '/auth/logout', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token()}` },
  }).catch(() => {})
  localStorage.removeItem(TOKEN_KEY)
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
}

export async function runGC(): Promise<GCResult> {
  return req('/gc', { method: 'POST' })
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return req('/auth/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

export interface HealthInfo {
  status: string
  mode: 'embedded' | 'proxy'
  registry?: string
  version?: string
}

export async function getHealth(): Promise<HealthInfo> {
  const res = await fetch('/health')
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}
