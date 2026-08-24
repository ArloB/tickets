// Thin fetch-based API client, hand-written rather than generated
// from api/openapi.yaml — mirroring internal/apiclient's own choice to
// stay hand-written (see that package's doc comment): the surface this
// web UI calls is large but bounded and known in advance, and
// generated-client tooling adds a build-time dependency for
// schema-drift protection the server's own OpenAPI-conformance tests
// already partially give from the other side. Revisit only if drift
// becomes a real, observed pain point.

/** The wire shape docs/contracts/errors.md defines — the exhaustive
 * field list, nothing else on any code. */
export interface ErrorBody {
  code: string
  message: string
  field: string | null
  correlation_id: string
  current_version: number | null
}

/** Thrown for any non-2xx response. Callers branch on `.code`, never
 * on `.message` (docs/contracts/errors.md). */
export class ApiError extends Error {
  code: string
  field: string | null
  correlationId: string
  currentVersion: number | null
  status: number

  constructor(body: ErrorBody, status: number) {
    super(body.message)
    this.name = 'ApiError'
    this.code = body.code
    this.field = body.field
    this.correlationId = body.correlation_id
    this.currentVersion = body.current_version
    this.status = status
  }
}

/** In-memory only, never localStorage — there is no reason to persist
 * a CSRF token across reloads: a fresh GET /auth/me after reload
 * returns a live session's token again (internal/httpapi/auth.go's
 * meResponse.csrf_token, added alongside this client specifically so
 * a reload doesn't force re-login). Module-level state is deliberate
 * here, not a component concern — every request in the app shares one
 * token for one active session. */
let csrfToken: string | null = null

export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

/** `If-Match: "<version>"` — a double-quoted decimal integer per
 * docs/contracts/concurrency.md (internal/httpapi/concurrency.go's
 * parseIfMatch rejects anything else, including an unquoted number). */
export function ifMatchHeader(version: number): Record<string, string> {
  return { 'If-Match': `"${version}"` }
}

/** apiFetch issues one request against /api/v1, attaching the session
 * cookie (`credentials: 'include'`) and, whenever we hold one, the
 * in-memory CSRF token. This is *not* limited to mutating methods:
 * internal/httpapi/auth_middleware.go's requireEditor checks
 * X-CSRF-Token for every session-authenticated request it wraps,
 * regardless of HTTP verb — most GETs are routeViewer and never see
 * this check, but a handful of admin-only reads (GET /agents, GET
 * /agents/{name}/tokens) are routeAdmin, which composes requireEditor,
 * so a plain read there 403s without the header too. Sending the
 * header on every request is harmless for the routes that don't check
 * it. Throws ApiError on any non-2xx response; a network-level failure
 * (server unreachable) propagates as whatever fetch itself throws —
 * callers that need to tell the two apart check `err instanceof
 * ApiError`. */
export async function apiFetch<T>(
  path: string,
  init: { method?: string; body?: unknown; headers?: Record<string, string> } = {},
): Promise<T> {
  const method = init.method ?? 'GET'
  const headers: Record<string, string> = { ...init.headers }
  let body: string | undefined

  if (init.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(init.body)
  }
  if (csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }

  const resp = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body,
    credentials: 'include',
  })

  if (resp.status === 204) {
    return undefined as T
  }

  const contentType = resp.headers.get('Content-Type') ?? ''
  const payload = contentType.includes('application/json') ? await resp.json() : undefined

  if (!resp.ok) {
    const errorBody: ErrorBody = payload?.error ?? {
      code: 'internal_error',
      message: `request failed with status ${resp.status}`,
      field: null,
      correlation_id: '',
      current_version: null,
    }
    throw new ApiError(errorBody, resp.status)
  }

  return payload as T
}

export async function apiFetchMultipart<T>(
  path: string,
  init: { method?: string; form: FormData; headers?: Record<string, string> },
): Promise<T> {
  const headers: Record<string, string> = { ...init.headers }
  if (csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }

  const resp = await fetch(`/api/v1${path}`, {
    method: init.method ?? 'POST',
    headers,
    body: init.form,
    credentials: 'include',
  })

  if (resp.status === 204) {
    return undefined as T
  }

  const contentType = resp.headers.get('Content-Type') ?? ''
  const payload = contentType.includes('application/json') ? await resp.json() : undefined

  if (!resp.ok) {
    const errorBody: ErrorBody = payload?.error ?? {
      code: 'internal_error',
      message: `request failed with status ${resp.status}`,
      field: null,
      correlation_id: '',
      current_version: null,
    }
    throw new ApiError(errorBody, resp.status)
  }

  return payload as T
}
