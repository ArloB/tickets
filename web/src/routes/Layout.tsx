import { Link, Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

/** Gate for every route except /login. `me === null` after `ready`
 * means a real 401 (anonymousRead off, no session) — redirect to
 * sign-in. A non-null `me` covers both anonymous-viewer mode
 * (permission: "viewer") and a signed-in editor; mutating controls
 * are Milestone 3's concern, not this milestone's. */
export default function Layout() {
  const { me, ready, bootstrapError, logout } = useAuth()

  if (!ready) return <p>Loading…</p>
  if (bootstrapError) return <p role="alert">Could not reach the server: {bootstrapError}</p>
  if (!me) return <Navigate to="/login" replace />

  return (
    <div>
      <nav>
        <Link to="/">Projects</Link>
        <span>
          {me.permission}
          {me.actor ? ` (${me.actor})` : ' (anonymous)'}
        </span>
        {/* me.actor is only ever empty for anonymous viewer mode — every
         * session cookie resolves to editor permission with a real actor
         * (internal/httpapi/auth_middleware.go's resolvePrincipal: no
         * such thing as a session-authenticated viewer). Safe to gate
         * Sign-out on actor rather than a separate "has session" flag. */}
        {me.actor && <button onClick={() => void logout()}>Sign out</button>}
      </nav>
      <Outlet />
    </div>
  )
}
