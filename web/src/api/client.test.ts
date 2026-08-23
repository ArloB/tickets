import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, apiFetch, setCsrfToken } from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('apiFetch', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setCsrfToken(null)
  })

  it('returns the decoded JSON body on success', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse(200, { permission: 'viewer', is_admin: false })),
    )

    const result = await apiFetch<{ permission: string }>('/auth/me')
    expect(result.permission).toBe('viewer')
  })

  it('throws ApiError with the envelope fields on a non-2xx response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        jsonResponse(409, {
          error: {
            code: 'version_conflict',
            message: 'stale version',
            field: null,
            correlation_id: 'corr-1',
            current_version: 4,
          },
        }),
      ),
    )

    await expect(apiFetch('/tickets/ABC-1')).rejects.toMatchObject({
      code: 'version_conflict',
      currentVersion: 4,
      correlationId: 'corr-1',
    })
  })

  it('attaches X-CSRF-Token on every request once a token is set, including GET', async () => {
    // A fresh Response per call — mockResolvedValue would reuse one
    // Response instance across both calls below, and a Response body
    // can only be read once.
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse(200, { status: 'ok' })))
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-abc')

    await apiFetch('/auth/logout', { method: 'POST' })
    const postHeaders = fetchMock.mock.calls[0][1].headers as Record<string, string>
    expect(postHeaders['X-CSRF-Token']).toBe('csrf-abc')

    // Not just mutating methods: internal/httpapi/auth_middleware.go's
    // requireEditor checks X-CSRF-Token for every session-authenticated
    // request it wraps, regardless of verb — GET /agents and GET
    // /agents/{name}/tokens are routeAdmin (composes requireEditor), so
    // a plain read there 403s without the header too.
    await apiFetch('/agents')
    const getHeaders = fetchMock.mock.calls[1][1].headers as Record<string, string>
    expect(getHeaders['X-CSRF-Token']).toBe('csrf-abc')
  })

  it('omits X-CSRF-Token when no token has been set yet', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { status: 'ok' }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/auth/me')
    const headers = fetchMock.mock.calls[0][1].headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBeUndefined()
  })

  it('sends credentials: include on every request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, {}))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/auth/me')
    expect(fetchMock.mock.calls[0][1].credentials).toBe('include')
  })
})

describe('ApiError', () => {
  it('is distinguishable from a network-level failure', () => {
    const err = new ApiError(
      { code: 'not_found', message: 'nope', field: null, correlation_id: '', current_version: null },
      404,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toBeInstanceOf(Error)
  })
})
