import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getProject } from '../api/projects'
import { listFeatures } from '../api/features'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import type { FeatureCompact, ProjectDetail } from '../api/types'

export default function ProjectOverview() {
  const { key = '' } = useParams()
  const [project, setProject] = useState<ProjectDetail | null>(null)
  const [features, setFeatures] = useState<FeatureCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setProject(null)
    setFeatures(null)
    setError(null)
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
        <Link to={`/projects/${key}/backlog`}>View backlog</Link>
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
    </main>
  )
}
