import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getDecision } from '../api/decisions'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listBacklinks } from '../api/backlinks'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { DecisionFieldsForm } from '../components/DecisionFieldsForm'
import { AssociationsSection } from '../components/AssociationsSection'
import { LinksSection } from '../components/LinksSection'
import { useAuth } from '../auth/AuthContext'
import type { Backlink, DecisionDetail as DecisionDetailDto, ExternalLink } from '../api/types'

export default function DecisionDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [decision, setDecision] = useState<DecisionDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    setDecision(null)
    setLinks(null)
    setAssociated(null)
    setBacklinks(null)
    setError(null)
    setEditing(false)
    Promise.all([getDecision(ref), listLinks(ref), listAssociations(ref), listBacklinks(ref)])
      .then(([d, l, a, b]) => {
        setDecision(d)
        setLinks(l.links)
        setAssociated(a.associated)
        setBacklinks(b.backlinks)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  if (error) return <p role="alert">{error}</p>
  if (!decision) return <p>Loading decision…</p>

  const canEdit = me?.permission === 'editor'

  if (editing) {
    return (
      <main>
        <h1>
          Edit {decision.title} <span>({decision.ref})</span>
        </h1>
        <DecisionFieldsForm
          decision={decision}
          onSaved={(updated) => {
            setDecision({ ...decision, ...updated })
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
        {decision.title} <span>({decision.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${decision.project}`}>{decision.project}</Link> ·{' '}
        {decision.status}
      </p>
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}

      <h2>Context</h2>
      <Markdown>{decision.context}</Markdown>

      <h2>Decision</h2>
      <Markdown>{decision.decision}</Markdown>

      <h2>Rationale</h2>
      <Markdown>{decision.rationale}</Markdown>

      <h2>Associations</h2>
      {associated && (
        <AssociationsSection
          entityRef={decision.ref}
          associated={associated}
          onChange={setAssociated}
          canEdit={canEdit}
        />
      )}

      <h2>Links</h2>
      {links && (
        <LinksSection entityRef={decision.ref} links={links} onChange={setLinks} canEdit={canEdit} />
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
