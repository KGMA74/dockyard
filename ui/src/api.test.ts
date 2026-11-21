import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, downloadScanReport, getRepositories } from './api'

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

describe('downloadScanReport()', () => {
  beforeEach(() => {
    localStorage.setItem('dockyard_token', 'access-token')
    URL.createObjectURL = vi.fn(() => 'blob:report')
    URL.revokeObjectURL = vi.fn()
  })
  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('fetches with the bearer token and saves under the server-given name', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('%PDF-1.4', {
        headers: {
          'content-type': 'application/pdf',
          'content-disposition': 'attachment; filename="dockyard-scan-team-app-1.0.pdf"',
        },
      }),
    )
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      expect(this.download).toBe('dockyard-scan-team-app-1.0.pdf')
      expect(this.href).toBe('blob:report')
    })

    await downloadScanReport(7, 'pdf')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toMatch(/^\/api\/admin\/scans\/7\/report\?format=pdf&lang=(en|fr)$/)
    expect((init?.headers as Record<string, string>).Authorization).toBe('Bearer access-token')
    expect(click).toHaveBeenCalledOnce()
    await new Promise(r => setTimeout(r, 0))
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:report')
  })

  it('surfaces server errors as ApiError, without starting a download', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(okJson({ error: 'report not found' }, 404))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click')
    await expect(downloadScanReport(7, 'csv')).rejects.toMatchObject({ status: 404, message: 'report not found' })
    expect(click).not.toHaveBeenCalled()
  })
})
