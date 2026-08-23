import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getFeature } from '../api/features'
import { listLinks } from '../api/links'
import { listBacklinks } from '../api/backlinks'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { FeatureFieldsForm } from '../components/FeatureFieldsForm'
import { useAuth } from '../auth/AuthContext'
import type { Backlink, ExternalLink, FeatureDetail as FeatureDetailDto } from '../api/types'

export default function FeatureDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [feature, setFeature] = useState<FeatureDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    setFeature(null)
    setLinks(null)
    setBacklinks(null)
    setError(null)
    setEditing(false)
    Promise.all([getFeature(ref), listLinks(ref), listBacklinks(ref)])
      .then(([f, l, b]) => {
        setFeature(f)
        setLinks(l.links)
        setBacklinks(b.backlinks)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  if (error) return <p role="alert">{error}</p>
  if (!feature) return <p>Loading feature…</p>

  if (editing) {
    return (
      <main>
        <h1>
          Edit {feature.title} <span>({feature.ref})</span>
        </h1>
        <FeatureFieldsForm
          feature={feature}
          onSaved={(updated) => {
            setFeature({ ...feature, ...updated })
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
        />
      </main>
    )
  }

  return (
    <main>
      <h1>
        {feature.title} <span>({feature.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${feature.project}`}>{feature.project}</Link>
      </p>
      <p>
        {feature.status} · {feature.priority}
      </p>
      {me?.permission === 'editor' && <button onClick={() => setEditing(true)}>Edit</button>}

      <h2>Description</h2>
      <Markdown>{feature.description}</Markdown>

      <h2>Links</h2>
      {!links || links.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {links.map((l) => (
            <li key={l.id}>
              <a href={l.url} target="_blank" rel="noreferrer">
                {l.title}
              </a>
            </li>
          ))}
        </ul>
      )}

      <h2>Backlinks</h2>
      {!backlinks || backlinks.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {backlinks.map((b) => (
            <li key={`${b.ref}-${b.comment_id ?? 'body'}`}>
              <Link to={detailRoute(b.ref)}>{b.ref}</Link>
              {b.comment_id !== undefined ? ` (comment #${b.comment_id})` : ' (description)'}
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
