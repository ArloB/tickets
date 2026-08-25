import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { createProject, listProjects } from '../api/projects'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import type { ProjectCompact } from '../api/types'

function NewProjectForm({ onCreated }: { onCreated: (p: ProjectCompact) => void }) {
  const [key, setKey] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createProject({ key, title, description })
      onCreated({
        key: created.key,
        title: created.title,
        status: created.status,
        version: created.version,
        updated_at: created.updated_at,
      })
      navigate(`/projects/${created.key}`)
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
        Key
        <input value={key} onChange={(e) => setKey(e.target.value)} placeholder="ABC" required />
      </label>
      <label>
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Description
        <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={4} />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create project'}
      </button>
    </form>
  )
}

export default function ProjectList() {
  const { me } = useAuth()
  const [projects, setProjects] = useState<ProjectCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  // Archived projects are hidden by default (ADR 0021: visibility
  // only, matching the server's own default) and shown on request
  // rather than always-visible — the list is for active work first.
  const [includeArchived, setIncludeArchived] = useState(false)

  useEffect(() => {
    setProjects(null)
    setError(null)
    listProjects(undefined, includeArchived)
      .then((page) => setProjects(page.projects))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [includeArchived])

  if (error) return <p role="alert">{error}</p>
  if (!projects) return <p>Loading projects…</p>

  return (
    <main>
      <h1>Projects</h1>
      <label>
        <input
          type="checkbox"
          checked={includeArchived}
          onChange={(e) => setIncludeArchived(e.target.checked)}
        />{' '}
        Include archived
      </label>
      {projects.length === 0 ? (
        <p>No projects yet.</p>
      ) : (
        <ul>
          {projects.map((p) => (
            <li key={p.key}>
              <Link to={`/projects/${p.key}`}>{p.title}</Link> <span>({p.key})</span>{' '}
              <span>{p.status}</span>
            </li>
          ))}
        </ul>
      )}

      {me?.permission === 'editor' &&
        (creating ? (
          <NewProjectForm onCreated={(p) => setProjects([...projects, p])} />
        ) : (
          <button onClick={() => setCreating(true)}>New project</button>
        ))}
    </main>
  )
}
