import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import {
  createAgent,
  createAgentToken,
  listAgents,
  listAgentTokens,
  revokeAgentToken,
  type AgentDetail,
  type AgentTokenSummary,
} from '../api/agents'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

function NewAgentForm({ onCreated }: { onCreated: (a: AgentDetail) => void }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createAgent({ name, description })
      onCreated(created)
      setName('')
      setDescription('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <label>
        Name
        <input value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label>
        Description
        <input value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create agent'}
      </button>
    </form>
  )
}

/** A token's raw value is returned exactly once, at creation (ADR
 * 0004) — this banner is the only place in the app that ever holds
 * one, and it's dropped as soon as the admin dismisses it or creates
 * another. Never stored in the token list state, which only ever
 * carries AgentTokenSummary (no `token` field at all). */
function NewTokenBanner({ token, onDismiss }: { token: string; onDismiss: () => void }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(token)
      setCopied(true)
    } catch {
      // Clipboard access can be denied or unavailable — the token is
      // still shown selectable in the input below, so this isn't fatal.
    }
  }

  return (
    <div role="alert">
      <p>
        <strong>This token will not be shown again.</strong> Copy it now.
      </p>
      <input value={token} readOnly onFocus={(e) => e.target.select()} />
      <button type="button" onClick={() => void copy()}>
        {copied ? 'Copied' : 'Copy'}
      </button>
      <button type="button" onClick={onDismiss}>
        Dismiss
      </button>
    </div>
  )
}

function AgentTokens({ agentName }: { agentName: string }) {
  const [tokens, setTokens] = useState<AgentTokenSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [description, setDescription] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [creating, setCreating] = useState(false)
  const [newToken, setNewToken] = useState<string | null>(null)

  function refresh() {
    setTokens(null)
    listAgentTokens(agentName)
      .then((page) => setTokens(page.tokens))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(refresh, [agentName])

  async function submitNewToken() {
    setCreating(true)
    setError(null)
    try {
      const created = await createAgentToken(agentName, {
        description,
        expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined,
      })
      setNewToken(created.token)
      setDescription('')
      setExpiresAt('')
      refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  async function revoke(id: number) {
    setError(null)
    try {
      await revokeAgentToken(agentName, id)
      refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <div>
      {error && <p role="alert">{error}</p>}
      {newToken && <NewTokenBanner token={newToken} onDismiss={() => setNewToken(null)} />}
      {!tokens ? (
        <p>Loading tokens…</p>
      ) : tokens.length === 0 ? (
        <p>No tokens yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Description</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((t) => (
              <tr key={t.id}>
                <td>{t.description}</td>
                <td>{t.created_at}</td>
                <td>{t.expires_at ?? 'never'}</td>
                <td>{t.revoked_at ? `revoked ${t.revoked_at}` : 'active'}</td>
                <td>
                  {!t.revoked_at && (
                    <button type="button" onClick={() => void revoke(t.id)}>
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submitNewToken()
        }}
      >
        <label>
          Description
          <input value={description} onChange={(e) => setDescription(e.target.value)} required />
        </label>
        <label>
          Expires
          <input
            type="datetime-local"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
          />
        </label>
        <button type="submit" disabled={creating}>
          {creating ? 'Creating…' : 'Create token'}
        </button>
      </form>
    </div>
  )
}

/** Agent/token administration (product spec §4.1) — admin-only, gated
 * both by nav visibility (Layout.tsx) and this route's own redirect,
 * since a non-admin editor could otherwise reach it by URL directly. */
export default function AdminAgents() {
  const { me } = useAuth()
  const [agents, setAgents] = useState<AgentDetail[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)

  useEffect(() => {
    listAgents()
      .then((page) => setAgents(page.agents))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  if (!me?.is_admin) return <Navigate to="/" replace />
  if (error) return <p role="alert">{error}</p>
  if (!agents) return <p>Loading agents…</p>

  return (
    <main>
      <h1>Agents</h1>
      {agents.length === 0 ? (
        <p>No agents yet.</p>
      ) : (
        <ul>
          {agents.map((a) => (
            <li key={a.name}>
              <button type="button" onClick={() => setExpanded(expanded === a.name ? null : a.name)}>
                {a.name}
              </button>{' '}
              <span>{a.description}</span> {a.owner && <span>owner: {a.owner}</span>}
              {expanded === a.name && <AgentTokens agentName={a.name} />}
            </li>
          ))}
        </ul>
      )}
      <NewAgentForm onCreated={(a) => setAgents([...(agents ?? []), a])} />
    </main>
  )
}
