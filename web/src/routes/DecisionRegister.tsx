import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { createDecision, listDecisions } from '../api/decisions'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import type { DecisionCompact } from '../api/types'

function NewDecisionForm({
  projectKey,
  onCreated,
}: {
  projectKey: string
  onCreated: (d: DecisionCompact) => void
}) {
  const [title, setTitle] = useState('')
  const [context, setContext] = useState('')
  const [decision, setDecision] = useState('')
  const [rationale, setRationale] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createDecision(projectKey, { title, context, decision, rationale })
      onCreated({
        ref: created.ref,
        title: created.title,
        status: created.status,
        version: created.version,
        updated_at: created.updated_at,
      })
      setTitle('')
      setContext('')
      setDecision('')
      setRationale('')
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
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Context
        <textarea value={context} onChange={(e) => setContext(e.target.value)} rows={4} />
      </label>
      <label>
        Decision
        <textarea value={decision} onChange={(e) => setDecision(e.target.value)} rows={4} />
      </label>
      <label>
        Rationale
        <textarea value={rationale} onChange={(e) => setRationale(e.target.value)} rows={4} />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create decision'}
      </button>
    </form>
  )
}

export default function DecisionRegister() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [decisions, setDecisions] = useState<DecisionCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    listDecisions(key)
      .then((page) => setDecisions(page.decisions))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [key])

  if (error) return <p role="alert">{error}</p>
  if (!decisions) return <p>Loading decisions…</p>

  return (
    <main>
      <h1>Decisions — {key}</h1>
      {decisions.length === 0 ? (
        <p>No decisions yet.</p>
      ) : (
        <ul>
          {decisions.map((d) => (
            <li key={d.ref}>
              <Link to={`/decisions/${d.ref}`}>{d.title}</Link> <span>({d.ref})</span>{' '}
              <span>{d.status}</span>
            </li>
          ))}
        </ul>
      )}

      {me?.permission === 'editor' &&
        (creating ? (
          <NewDecisionForm
            projectKey={key}
            onCreated={(d) => {
              setDecisions([...decisions, d])
              setCreating(false)
            }}
          />
        ) : (
          <button onClick={() => setCreating(true)}>New decision</button>
        ))}
    </main>
  )
}
