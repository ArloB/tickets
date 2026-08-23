import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getProject } from '../api/projects'
import { createFeature, listFeatures } from '../api/features'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { useAuth } from '../auth/AuthContext'
import type { FeatureCompact, Priority, ProjectDetail } from '../api/types'

const priorities: Priority[] = ['critical', 'high', 'medium', 'low']

function NewFeatureForm({
  projectKey,
  onCreated,
}: {
  projectKey: string
  onCreated: (f: FeatureCompact) => void
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

export default function ProjectOverview() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [project, setProject] = useState<ProjectDetail | null>(null)
  const [features, setFeatures] = useState<FeatureCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    setProject(null)
    setFeatures(null)
    setError(null)
    setCreating(false)
    Promise.all([getProject(key), listFeatures(key)])
      .then(([proj, page]) => {
        setProject(proj)
        setFeatures(page.features)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [key])

  if (error) return <p role="alert">{error}</p>
  if (!project) return <p>Loading project…</p>

  return (
    <main>
      <h1>
        {project.title} <span>({project.key})</span>
      </h1>
      <p>Status: {project.status}</p>
      <Markdown>{project.description}</Markdown>
      <p>
        <Link to={`/projects/${key}/backlog`}>View backlog</Link> ·{' '}
        <Link to={`/projects/${key}/decisions`}>View decisions</Link>
      </p>
      <h2>Features</h2>
      {!features || features.length === 0 ? (
        <p>No features yet.</p>
      ) : (
        <ul>
          {features.map((f) => (
            <li key={f.ref}>
              <Link to={`/features/${f.ref}`}>{f.title}</Link> <span>({f.ref})</span>{' '}
              <span>{f.status}</span> <span>{f.priority}</span>
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
    </main>
  )
}
