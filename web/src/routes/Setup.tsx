import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { setupAdmin } from '../api/auth'
import { createAgent, createAgentToken } from '../api/agents'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { FundamentalsList } from '../components/Fundamentals'

function CreateAdminStep() {
  const { login } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [alreadySetUp, setAlreadySetUp] = useState(false)
  const [createdButNotSignedIn, setCreatedButNotSignedIn] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (password !== confirm) {
      setError('Passwords do not match.')
      return
    }
    setSubmitting(true)
    try {
      await setupAdmin(username, password)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'already_exists') {
        setAlreadySetUp(true)
      } else {
        setError(err instanceof ApiError ? err.message : String(err))
      }
      setSubmitting(false)
      return
    }
    try {
      await login(username, password)
    } catch {
      setCreatedButNotSignedIn(true)
    } finally {
      setSubmitting(false)
    }
  }

  if (alreadySetUp) {
    return (
      <p role="alert">
        This server already has an admin account. <Link to="/login">Sign in instead</Link>.
      </p>
    )
  }

  if (createdButNotSignedIn) {
    return (
      <p role="alert">
        The admin account was created, but signing in failed.{' '}
        <Link to="/login">Sign in</Link> with the username and password you just set.
      </p>
    )
  }

  return (
    <>
      <p className="auth-mark">Step 1 of 3</p>
      <h1>Create the admin account</h1>
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
            autoComplete="new-password"
            required
          />
        </label>
        <label>
          Confirm password
          <input
            type="password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
            required
          />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={submitting}>
          {submitting ? 'Creating…' : 'Create account'}
        </button>
      </form>
    </>
  )
}

function TokenStep({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState('cli')
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [token, setToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await createAgent({ name, description })
      const created = await createAgentToken(name, { description: 'created during first-run setup' })
      setToken(created.token)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  async function copy() {
    if (!token) return
    try {
      await navigator.clipboard.writeText(token)
      setCopied(true)
    } catch {}
  }

  if (token) {
    return (
      <>
        <p className="auth-mark">Step 2 of 3</p>
        <h1>Your first token</h1>
        <div role="alert">
          <p>
            <strong>This token will not be shown again.</strong> Copy it now.
          </p>
          <input aria-label="New token value" value={token} readOnly onFocus={(e) => e.target.select()} />
          <button type="button" onClick={() => void copy()}>
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
        <button type="button" onClick={onDone}>
          Continue
        </button>
      </>
    )
  }

  return (
    <>
      <p className="auth-mark">Step 2 of 3</p>
      <h1>Generate your first token</h1>
      <p>
        Tickets is built for coding agents as well as people — an agent authenticates with a bearer
        token instead of a password. Create one now, or skip and do this later from Admin → Agents.
      </p>
      <form onSubmit={(e) => void handleSubmit(e)}>
        <label>
          Agent name
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label>
          Description
          <input value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        {error && <p role="alert">{error}</p>}
        <div className="form-actions">
          <button type="submit" disabled={submitting}>
            {submitting ? 'Creating…' : 'Create token'}
          </button>
          <button type="button" onClick={onDone}>
            Skip
          </button>
        </div>
      </form>
    </>
  )
}

function WalkthroughStep({ onDone }: { onDone: () => void }) {
  return (
    <>
      <p className="auth-mark">Step 3 of 3</p>
      <h1>The fundamentals</h1>
      <FundamentalsList />
      <p>
        This is also reachable any time from the "?" link in the header — no need to remember any of
        it now.
      </p>
      <button type="button" onClick={onDone}>
        Finish
      </button>
    </>
  )
}

export default function Setup() {
  const { me, ready } = useAuth()
  const navigate = useNavigate()
  const [tokenStepDone, setTokenStepDone] = useState(false)

  if (!ready) return <p>Loading…</p>
  if (me?.actor && !me.is_admin) return <Navigate to="/" replace />

  return (
    <main className="centered-form auth-card">
      {!me?.is_admin ? (
        <CreateAdminStep />
      ) : !tokenStepDone ? (
        <TokenStep onDone={() => setTokenStepDone(true)} />
      ) : (
        <WalkthroughStep onDone={() => navigate('/', { replace: true })} />
      )}
    </main>
  )
}
