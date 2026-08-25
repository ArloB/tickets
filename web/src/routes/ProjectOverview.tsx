import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getProjectBrief } from '../api/projectBrief'
import { createFeature } from '../api/features'
import { listComments } from '../api/comments'
import { updateProjectStatus } from '../api/projects'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { CommentsSection } from '../components/CommentsSection'
import { ProjectFieldsForm } from '../components/ProjectFieldsForm'
import { useAuth } from '../auth/AuthContext'
import type {
  ActivityEvent,
  CommentDetail,
  DecisionCompact,
  ContentItemCompact,
  FeatureBriefRow,
  Priority,
  ProjectDetail,
  TicketCompact,
} from '../api/types'

const priorities: Priority[] = ['critical', 'high', 'medium', 'low']

function NewFeatureForm({
  projectKey,
  onCreated,
}: {
  projectKey: string
  onCreated: (f: FeatureBriefRow) => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState<Priority>('medium')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createFeature(projectKey, { title, description, priority })
      onCreated({
        ref: created.ref,
        title: created.title,
        status: created.status,
        priority: created.priority,
        version: created.version,
        updated_at: created.updated_at,
        tickets_total: 0,
        tickets_done: 0,
      })
      setTitle('')
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
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Description
        <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={4} />
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
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create feature'}
      </button>
    </form>
  )
}

function TicketBriefList({ tickets }: { tickets: TicketCompact[] }) {
  if (tickets.length === 0) return <p>None.</p>
  return (
    <ul>
      {tickets.map((t) => (
        <li key={t.ref}>
          <Link to={`/tickets/${t.ref}`}>{t.title}</Link> <span>({t.ref})</span> <span>{t.status}</span>{' '}
          <span>{t.priority}</span>
        </li>
      ))}
    </ul>
  )
}

function DecisionBriefList({ decisions }: { decisions: DecisionCompact[] }) {
  if (decisions.length === 0) return <p>None yet.</p>
  return (
    <ul>
      {decisions.map((d) => (
        <li key={d.ref}>
          <Link to={`/decisions/${d.ref}`}>{d.title}</Link> <span>({d.ref})</span>
        </li>
      ))}
    </ul>
  )
}

function PlanBriefList({ plans }: { plans: ContentItemCompact[] }) {
  if (plans.length === 0) return <p>None yet.</p>
  return (
    <ul>
      {plans.map((p) => (
        <li key={p.ref}>
          <Link to={`/plans/${p.ref}`}>{p.title}</Link> <span>({p.ref})</span>
        </li>
      ))}
    </ul>
  )
}

function ActivityBriefList({ events }: { events: ActivityEvent[] }) {
  if (events.length === 0) return <p>No recent activity.</p>
  return (
    <ul>
      {events.map((e) => (
        <li key={e.id}>
          <span>{e.actor}</span> {e.event_type}
          {e.entity ? (
            <>
              {' '}
              on <span>{e.entity}</span>
            </>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

export default function ProjectOverview() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [project, setProject] = useState<ProjectDetail | null>(null)
  const [inProgress, setInProgress] = useState<TicketCompact[]>([])
  const [issueRegister, setIssueRegister] = useState<TicketCompact[]>([])
  const [features, setFeatures] = useState<FeatureBriefRow[] | null>(null)
  const [recentDecisions, setRecentDecisions] = useState<DecisionCompact[]>([])
  const [recentPlans, setRecentPlans] = useState<ContentItemCompact[]>([])
  const [recentActivity, setRecentActivity] = useState<ActivityEvent[]>([])
  const [comments, setComments] = useState<CommentDetail[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(false)
  const [statusBusy, setStatusBusy] = useState(false)
  const [statusError, setStatusError] = useState<string | null>(null)

  useEffect(() => {
    setProject(null)
    setFeatures(null)
    setComments(null)
    setError(null)
    setCreating(false)
    setEditing(false)
    setStatusError(null)
    Promise.all([getProjectBrief(key), listComments(key)])
      .then(([brief, c]) => {
        setProject(brief.project)
        setInProgress(brief.in_progress)
        setIssueRegister(brief.issue_register)
        setFeatures(brief.features)
        setRecentDecisions(brief.recent_decisions)
        setRecentPlans(brief.recent_plans)
        setRecentActivity(brief.recent_activity)
        setComments(c.comments)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [key])

  if (error) return <p role="alert">{error}</p>
  if (!project) return <p>Loading project…</p>

  const canEdit = me?.permission === 'editor'

  if (editing) {
    return (
      <main>
        <h1>
          Edit {project.title} <span>({project.key})</span>
        </h1>
        <ProjectFieldsForm
          project={project}
          onSaved={(updated) => {
            setProject(updated)
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
        />
      </main>
    )
  }

  async function toggleArchived() {
    if (!project) return
    setStatusBusy(true)
    setStatusError(null)
    try {
      const updated = await updateProjectStatus(
        project.key,
        project.status === 'archived' ? 'active' : 'archived',
        project.version,
      )
      setProject(updated)
    } catch (err) {
      setStatusError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setStatusBusy(false)
    }
  }

  return (
    <main>
      <h1>
        {project.title} <span>({project.key})</span>
      </h1>
      <p>
        Status: {project.status}
        {canEdit && (
          <>
            {' '}
            · <button onClick={() => setEditing(true)}>Edit</button>{' '}
            <button onClick={() => void toggleArchived()} disabled={statusBusy}>
              {statusBusy
                ? 'Working…'
                : project.status === 'archived'
                  ? 'Unarchive'
                  : 'Archive'}
            </button>
          </>
        )}
      </p>
      {statusError && <p role="alert">{statusError}</p>}
      <Markdown>{project.description}</Markdown>
      <p>
        <Link to={`/projects/${key}/backlog`}>View backlog</Link> ·{' '}
        <Link to={`/projects/${key}/board`}>Ticket board</Link> ·{' '}
        <Link to={`/projects/${key}/features/board`}>Feature board</Link> ·{' '}
        <Link to={`/projects/${key}/decisions`}>View decisions</Link> ·{' '}
        <Link to={`/projects/${key}/plans`}>Plans</Link> ·{' '}
        <Link to={`/projects/${key}/documents`}>Documents</Link> ·{' '}
        <Link to={`/projects/${key}/activity`}>Activity</Link>
      </p>

      <h2>In progress / upcoming</h2>
      <TicketBriefList tickets={inProgress} />

      <h2>Issue register</h2>
      <TicketBriefList tickets={issueRegister} />

      <h2>Features</h2>
      {!features || features.length === 0 ? (
        <p>No features yet.</p>
      ) : (
        <ul>
          {features.map((f) => (
            <li key={f.ref}>
              <Link to={`/features/${f.ref}`}>{f.title}</Link> <span>({f.ref})</span>{' '}
              <span>{f.status}</span> <span>{f.priority}</span>{' '}
              <span>
                {f.tickets_done}/{f.tickets_total} done
              </span>
            </li>
          ))}
        </ul>
      )}

      {me?.permission === 'editor' && features &&
        (creating ? (
          <NewFeatureForm
            projectKey={key}
            onCreated={(f) => {
              setFeatures([...features, f])
              setCreating(false)
            }}
          />
        ) : (
          <button onClick={() => setCreating(true)}>New feature</button>
        ))}

      <h2>Recent decisions</h2>
      <DecisionBriefList decisions={recentDecisions} />

      <h2>Recent plans</h2>
      <PlanBriefList plans={recentPlans} />

      <h2>Recent activity</h2>
      <ActivityBriefList events={recentActivity} />

      <h2>Comments</h2>
      {comments && (
        <CommentsSection
          entityRef={key}
          comments={comments}
          onChange={setComments}
          canEdit={me?.permission === 'editor'}
        />
      )}
    </main>
  )
}
