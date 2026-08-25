import { useEffect, useState } from 'react'
import { Link, Navigate, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { connectChangeHints } from '../api/events'

/** Gate for every route except /login. `me === null` after `ready`
 * means a real 401 (anonymousRead off, no session) — redirect to
 * sign-in. A non-null `me` covers both anonymous-viewer mode
 * (permission: "viewer") and a signed-in editor; mutating controls
 * are Milestone 3's concern, not this milestone's. */
export default function Layout() {
  const { me, ready, bootstrapError, logout } = useAuth()
  const navigate = useNavigate()
  const [query, setQuery] = useState('')

  // Opened once the shell actually renders (i.e. never for the
  // sign-in page, which mounts SignIn directly, not Layout) — every
  // authenticated route shares this one connection for as long as the
  // tab is open (Phase 5 Step 8, ADR 0020).
  useEffect(() => {
    connectChangeHints()
  }, [])

  if (!ready) return <p>Loading…</p>
  if (bootstrapError) return <p role="alert">Could not reach the server: {bootstrapError}</p>
  if (!me) return <Navigate to="/login" replace />

  return (
    <div>
      <nav>
        <Link to="/">Projects</Link>
        {me.actor && <Link to="/notifications">Notifications</Link>}
        {me.actor?.startsWith('human:') && <Link to="/admin/accounts">Accounts</Link>}
        {me.is_admin && <Link to="/admin/agents">Agents</Link>}
        <form
          role="search"
          onSubmit={(e) => {
            e.preventDefault()
            const q = query.trim()
            if (q) navigate(`/search?q=${encodeURIComponent(q)}`)
          }}
        >
          <label>
            Search
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search tickets, features, decisions…"
            />
          </label>
        </form>
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
