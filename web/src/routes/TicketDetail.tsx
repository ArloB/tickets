import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getTicket } from '../api/tickets'
import { listBacklinks } from '../api/backlinks'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listAttachments } from '../api/attachments'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { TicketFieldsForm } from '../components/TicketFieldsForm'
import { TicketActions } from '../components/TicketActions'
import { CommentsSection } from '../components/CommentsSection'
import { RelationshipsSection } from '../components/RelationshipsSection'
import { AssociationsSection } from '../components/AssociationsSection'
import { LinksSection } from '../components/LinksSection'
import { AttachmentList } from '../components/AttachmentList'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  ExternalLink,
  TicketDetail as TicketDetailDto,
} from '../api/types'

export default function TicketDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [ticket, setTicket] = useState<TicketDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [attachments, setAttachments] = useState<Attachment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    setTicket(null)
    setLinks(null)
    setAssociated(null)
    setBacklinks(null)
    setAttachments(null)
    setError(null)
    setEditing(false)
    Promise.all([
      getTicket(ref, ['comments', 'relationships']),
      listLinks(ref),
      listAssociations(ref),
      listBacklinks(ref),
      listAttachments(ref),
    ])
      .then(([t, l, a, b, at]) => {
        setTicket(t)
        setLinks(l.links)
        setAssociated(a.associated)
        setBacklinks(b.backlinks)
        setAttachments(at.attachments)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  if (error) return <p role="alert">{error}</p>
  if (!ticket) return <p>Loading ticket…</p>

  const canEdit = me?.permission === 'editor'

  if (editing) {
    return (
      <main>
        <h1>
          Edit {ticket.title} <span>({ticket.ref})</span>
        </h1>
        <TicketFieldsForm
          ticket={ticket}
          onSaved={(updated) => {
            setTicket({ ...ticket, ...updated })
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
        {ticket.title} <span>({ticket.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${ticket.project}`}>{ticket.project}</Link>
        {ticket.feature && (
          <>
            {' '}
            · Feature: <Link to={`/features/${ticket.feature}`}>{ticket.feature}</Link>
          </>
        )}
      </p>
      <p>
        {ticket.type} · {ticket.status} · {ticket.priority}
        {ticket.severity ? ` · ${ticket.severity}` : ''}
      </p>
      <p>
        Assignee: {ticket.assignee ?? 'unassigned'} · Creator: {ticket.creator ?? 'unknown'}
      </p>
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}
      {canEdit && <TicketActions ticket={ticket} onUpdated={setTicket} />}

      <h2>Description</h2>
      <Markdown>{ticket.description}</Markdown>

      <h2>Relationships</h2>
      <RelationshipsSection
        ticketRef={ticket.ref}
        relationships={ticket.relationships ?? []}
        onChange={(relationships) => setTicket({ ...ticket, relationships })}
        canEdit={canEdit}
      />

      <h2>Associations</h2>
      {associated && (
        <AssociationsSection
          entityRef={ticket.ref}
          associated={associated}
          onChange={setAssociated}
          canEdit={canEdit}
        />
      )}

      <h2>Links</h2>
      {links && (
        <LinksSection entityRef={ticket.ref} links={links} onChange={setLinks} canEdit={canEdit} />
      )}

      <h2>Attachments</h2>
      {attachments && (
        <AttachmentList
          ownerRef={ticket.ref}
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

      <h2>Comments</h2>
      <CommentsSection
        ticketRef={ticket.ref}
        comments={ticket.comments ?? []}
        onChange={(comments) => setTicket({ ...ticket, comments })}
        canEdit={canEdit}
      />
    </main>
  )
}
