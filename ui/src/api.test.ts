import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getRepositories } from './api'

const okJson = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })

describe('req() error handling', () => {
  beforeEach(() => {
    localStorage.setItem('dockyard_token', 'header.eyJyb2xlIjoiYWRtaW4ifQ.sig')
  })
  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('throws a typed ApiError carrying the status and message', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(okJson({ error: 'nope' }, 403))
    await expect(getRepositories()).rejects.toMatchObject({
      name: 'ApiError',
      status: 403,
      message: 'nope',
    })
    await expect(getRepositories()).rejects.toBeInstanceOf(ApiError)
  })

  it('falls back to HTTP <status> for non-JSON error bodies', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('<html>502</html>', { status: 502, headers: { 'content-type': 'text/html' } }),
    )
    await expect(getRepositories()).rejects.toMatchObject({ status: 502, message: 'HTTP 502' })
  })

  it('refreshes once on 401 then retries', async () => {
    localStorage.setItem('dockyard_refresh', 'refresh-token')
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson({ error: 'expired' }, 401))
      .mockResolvedValueOnce(okJson({ token: 'new', refresh_token: 'new-r' }))
      .mockResolvedValueOnce(okJson({ repositories: [], total: 0 }))

    const res = await getRepositories()
    expect(res).toEqual({ repositories: [], total: 0 })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})
