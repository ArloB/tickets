import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listProjects } from '../api/projects'
import { ApiError } from '../api/client'
import type { ProjectCompact } from '../api/types'

export default function ProjectList() {
  const [projects, setProjects] = useState<ProjectCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listProjects()
      .then((page) => setProjects(page.projects))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  if (error) return <p role="alert">{error}</p>
  if (!projects) return <p>Loading projects…</p>

  return (
    <main>
      <h1>Projects</h1>
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
    </main>
  )
}
