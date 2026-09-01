import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { createTicket } from '../api/tickets'
import { listFeatures } from '../api/features'
import { ApiError } from '../api/client'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { useAuth } from '../auth/AuthContext'
import type { FeatureCompact, Priority, Severity, TicketType } from '../api/types'

const types: TicketType[] = ['task', 'bug', 'security', 'chore']
const severities: Severity[] = ['critical', 'high', 'medium', 'low']
const priorities: Priority[] = ['critical', 'high', 'medium', 'low']

/** Ticket creation as its own route rather than a form unfolding
 * inside the backlog list — a new ticket is a page's worth of fields
 * (description included), and the backlog only links here. */
export default function NewTicket() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()

  const [type, setType] = useState<TicketType>('task')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState<Priority>('medium')
  const [severity, setSeverity] = useState<Severity | ''>('')
  const [features, setFeatures] = useState<FeatureCompact[] | null>(null)
  // Pre-selects the feature when arriving from a feature's own page, so
  // that path doesn't make the user re-pick what they just came from.
  const [featureRef, setFeatureRef] = useState(params.get('feature_ref') ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const severityApplicable = type === 'bug' || type === 'security'

  // No default feature (ADR 0023) — a ticket has to be assigned to one
  // deliberately, General included, rather than silently landing there.
  useEffect(() => {
    let cancelled = false
    listFeatures(key, {}, undefined, 100)
      .then((page) => {
        if (!cancelled) setFeatures(page.features)
      })
      .catch(() => {
        if (!cancelled) setFeatures([])
      })
    return () => {
      cancelled = true
    }
  }, [key])

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createTicket(key, {
        type,
        title,
        description,
        priority,
        severity: severityApplicable && severity !== '' ? severity : null,
        feature: featureRef,
      })
      navigate(`/tickets/${created.ref}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
      setBusy(false)
    }
  }

  if (me && me.permission !== 'editor') {
    return (
      <main>
        <h1>New ticket — {key}</h1>
        <p role="alert">You need editor permission to create a ticket.</p>
        <p>
          <Link to={`/projects/${key}/backlog`}>Back to backlog</Link>
        </p>
      </main>
    )
  }

  return (
    <main>
      <h1>New ticket — {key}</h1>
      <p>
        <Link to={`/projects/${key}/backlog`}>← Back to backlog</Link>
      </p>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <label>
          Title
          <input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus />
        </label>
        <label>
          Type
          <select value={type} onChange={(e) => setType(e.target.value as TicketType)}>
            {types.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>
        <label>
          Feature
          <select
            value={featureRef}
            onChange={(e) => setFeatureRef(e.target.value)}
            required
            disabled={!features}
          >
            <option value="" disabled>
              {features ? 'Choose a feature…' : 'Loading features…'}
            </option>
            {features?.map((f) => (
              <option key={f.ref} value={f.ref}>
                {f.ref} — {f.title}
              </option>
            ))}
          </select>
        </label>
        <label>
          Priority
          <select value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
            {priorities.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </label>
        {severityApplicable && (
          <label>
            Severity
            <select value={severity} onChange={(e) => setSeverity(e.target.value as Severity | '')}>
              <option value="">None</option>
              {severities.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        )}
        <MarkdownEditor
          label="Description"
          value={description}
          onChange={setDescription}
          projectKey={key}
        />
        {error && <p role="alert">{error}</p>}
        <div className="form-actions">
          <button type="submit" disabled={busy}>
            {busy ? 'Creating…' : 'Create ticket'}
          </button>
          <Link to={`/projects/${key}/backlog`}>Cancel</Link>
        </div>
      </form>
    </main>
  )
}
