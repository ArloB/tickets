import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

export default function SignIn() {
  const { me, login } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username, password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'sign-in failed')
    } finally {
      setSubmitting(false)
    }
  }

  // me.actor is empty only for anonymous viewer mode, never for a real
  // session (see Layout.tsx's comment) — safe to treat as "already
  // signed in".
  if (me?.actor) return <Navigate to="/" replace />

  return (
    <main className="centered-form auth-card">
      <p className="auth-mark">Tickets</p>
      <h1>Sign in</h1>
      <form onSubmit={(e) => void handleSubmit(e)}>
        <label>
          Username
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={submitting}>
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <p>
        Setting up for the first time? <Link to="/setup">Create the admin account</Link>.
      </p>
    </main>
  )
}
