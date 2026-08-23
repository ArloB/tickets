import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { getMe, login as apiLogin, logout as apiLogout, type Me } from '../api/auth'
import { ApiError } from '../api/client'

interface AuthState {
  /** null while the initial GET /auth/me is in flight. */
  me: Me | null
  /** Set once the initial bootstrap resolves (successfully or with a
   * 401) — distinguishes "still loading" from "loaded, signed out". */
  ready: boolean
  /** Set when the initial bootstrap fails for a reason other than
   * "no session" (network error, 5xx) — the sign-in view surfaces
   * this rather than looping forever on "Connecting…". */
  bootstrapError: string | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthCtx = createContext<AuthState | null>(null)

/** Auth bootstrap per the Phase 4 plan (§3 "Auth bootstrap"): GET
 * /auth/me on load. A 401 here means the server has anonymousRead off
 * and this browser has no session — sign-in view. Any other outcome
 * (200 viewer or 200 editor) means `me` is set and the rest of the UI
 * renders read-only or full accordingly. */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [ready, setReady] = useState(false)
  const [bootstrapError, setBootstrapError] = useState<string | null>(null)

  useEffect(() => {
    getMe()
      .then((result) => setMe(result))
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          setMe(null)
          return
        }
        setBootstrapError(err instanceof ApiError ? `${err.code}: ${err.message}` : String(err))
      })
      .finally(() => setReady(true))
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    await apiLogin(username, password)
    setMe(await getMe())
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    setMe(null)
  }, [])

  return (
    <AuthCtx.Provider value={{ me, ready, bootstrapError, login, logout }}>
      {children}
    </AuthCtx.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthCtx)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
