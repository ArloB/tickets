import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getFeature } from '../api/features'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listBacklinks } from '../api/backlinks'
import { listAttachments } from '../api/attachments'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { FeatureFieldsForm } from '../components/FeatureFieldsForm'
import { AssociationsSection } from '../components/AssociationsSection'
import { LinksSection } from '../components/LinksSection'
import { AttachmentList } from '../components/AttachmentList'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  ExternalLink,
  FeatureDetail as FeatureDetailDto,
} from '../api/types'

export default function FeatureDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [feature, setFeature] = useState<FeatureDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [attachments, setAttachments] = useState<Attachment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    setFeature(null)
    setLinks(null)
    setAssociated(null)
    setBacklinks(null)
    setAttachments(null)
    setError(null)
    setEditing(false)
    Promise.all([
      getFeature(ref),
      listLinks(ref),
      listAssociations(ref),
      listBacklinks(ref),
      listAttachments(ref),
    ])
      .then(([f, l, a, b, at]) => {
        setFeature(f)
        setLinks(l.links)
        setAssociated(a.associated)
        setBacklinks(b.backlinks)
        setAttachments(at.attachments)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  if (error) return <p role="alert">{error}</p>
  if (!feature) return <p>Loading feature…</p>

  const canEdit = me?.permission === 'editor'

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
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}

      <h2>Description</h2>
      <Markdown>{feature.description}</Markdown>

      <h2>Associations</h2>
      {associated && (
        <AssociationsSection
          entityRef={feature.ref}
          associated={associated}
          onChange={setAssociated}
          canEdit={canEdit}
        />
      )}

      <h2>Links</h2>
      {links && (
        <LinksSection entityRef={feature.ref} links={links} onChange={setLinks} canEdit={canEdit} />
      )}

      <h2>Attachments</h2>
      {attachments && (
        <AttachmentList
          ownerRef={feature.ref}
          attachments={attachments}
          onChange={setAttachments}
          canEdit={canEdit}
        />
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
