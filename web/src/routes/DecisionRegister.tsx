import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { createDecision, listDecisions } from '../api/decisions'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Pager } from '../components/Pager'
import { StatusChip } from '../components/StatusChip'
import { useCursorPager } from '../hooks/useCursorPager'
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
  const [consequences, setConsequences] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createDecision(projectKey, { title, context, decision, rationale, consequences })
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
      setConsequences('')
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
      <label>
        Consequences
        <textarea value={consequences} onChange={(e) => setConsequences(e.target.value)} rows={4} />
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
  const [creating, setCreating] = useState(false)

  const {
    items: decisions,
    setItems: setDecisions,
    error,
    loading,
    hasNext,
    hasPrev,
    next,
    prev,
  } = useCursorPager<DecisionCompact>(
    (cursor) => listDecisions(key, cursor).then((page) => ({ items: page.decisions, nextCursor: page.next_cursor })),
    [key],
  )

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
              <StatusChip value={d.status} kind="decision" />
            </li>
          ))}
        </ul>
      )}
      <Pager hasPrev={hasPrev} hasNext={hasNext} loading={loading} onPrev={prev} onNext={next} />

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
