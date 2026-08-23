import { apiFetch, setCsrfToken } from './client'

/** Mirrors internal/httpapi/auth.go's meResponse. */
export interface Me {
  actor?: string
  permission: 'viewer' | 'editor'
  is_admin: boolean
  csrf_token?: string
}

/** Mirrors internal/httpapi/auth.go's loginResponse. */
export interface LoginResult {
  actor: string
  csrf_token: string
}

/** GET /auth/me — resolves the caller's own auth state (product spec
 * §4.2). Recovers and stashes the CSRF token for a session-
 * authenticated caller, so a page reload with a live session cookie
 * doesn't need a fresh login just to make its next mutating request. */
export async function getMe(): Promise<Me> {
  const me = await apiFetch<Me>('/auth/me')
  if (me.csrf_token) {
    setCsrfToken(me.csrf_token)
  }
  return me
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const result = await apiFetch<LoginResult>('/auth/login', {
    method: 'POST',
    body: { username, password },
  })
  setCsrfToken(result.csrf_token)
  return result
}

export async function logout(): Promise<void> {
  await apiFetch<void>('/auth/logout', { method: 'POST' })
  setCsrfToken(null)
}
