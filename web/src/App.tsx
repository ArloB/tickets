import { useEffect, useState } from 'react'
import { getMe, type Me } from './api/auth'
import { ApiError } from './api/client'

// Milestone 1's own exit bar (Phase 4 plan, §2 "Web UI project
// setup"): prove the whole chain end to end — dev-proxy or embedded
// static serving, through the Go API, back into rendered React state
// — with a trivial page, before Milestone 2 builds the real sign-in
// shell and read-only views on top of this same api/auth.ts module.
export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getMe()
      .then(setMe)
      .catch((err: unknown) => {
        setError(err instanceof ApiError ? `${err.code}: ${err.message}` : String(err))
      })
  }, [])

  return (
    <main style={{ padding: '2rem', fontFamily: 'inherit' }}>
      <h1>Tickets</h1>
      {error && <p style={{ color: 'crimson' }}>Could not reach the server: {error}</p>}
      {!error && !me && <p>Connecting…</p>}
      {me && (
        <p>
          Connected as {me.permission}
          {me.actor ? ` (${me.actor})` : ' (anonymous)'}.
        </p>
      )}
    </main>
  )
}
