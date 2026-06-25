const BASE = '/api/admin'
const TOKEN_KEY = 'maestro_token'

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

export function isAuthenticated(): boolean {
  return !!localStorage.getItem(TOKEN_KEY)
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
}

export async function getRepositories(): Promise<{ repositories: RepoSummary[]; total: number }> {
  return req('/repositories')
}

export interface TagInfo {
  tag: string
  digest: string
}

export async function getTags(name: string): Promise<{ name: string; tags: TagInfo[]; total: number }> {
  return req(`/repositories/tags?name=${encodeURIComponent(name)}`)
}

export async function deleteManifest(name: string, digest: string): Promise<void> {
  return req(`/repositories/manifests?name=${encodeURIComponent(name)}&digest=${encodeURIComponent(digest)}`, {
    method: 'DELETE',
  })
}

export interface StorageStats {
  total_size_bytes: number
  total_size_human: string
  blob_count: number
  repo_count: number
  storage_path?: string
}

export async function getStorageStats(): Promise<StorageStats> {
  return req('/storage/stats')
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
