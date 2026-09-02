import { useCallback, useEffect, useState } from 'react'
import { Link, Outlet, useOutletContext, useParams } from 'react-router-dom'
import { getTicket, restoreTicket } from '../api/tickets'
import { listBacklinks } from '../api/backlinks'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listAttachments } from '../api/attachments'
import { ApiError } from '../api/client'
import { useEntityChanged } from '../api/events'
import { Markdown } from '../components/Markdown'
import { TicketFieldsForm } from '../components/TicketFieldsForm'
import { TicketActions, DeleteTicketButton } from '../components/TicketActions'
import { CommentsSection } from '../components/CommentsSection'
import { DetailTabs } from '../components/DetailTabs'
import { LinksTabView } from '../components/LinksTabView'
import { AttachmentsTabView } from '../components/AttachmentsTabView'
import { SubscribeButton } from '../components/SubscribeButton'
import { StatusChip } from '../components/StatusChip'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  ExternalLink,
  TicketDetail as TicketDetailDto,
} from '../api/types'

interface TicketContext {
  ticket: TicketDetailDto
  setTicket: (ticket: TicketDetailDto) => void
  links: ExternalLink[]
  setLinks: (links: ExternalLink[]) => void
  associated: string[]
  setAssociated: (associated: string[]) => void
  backlinks: Backlink[]
  attachments: Attachment[]
  setAttachments: (attachments: Attachment[]) => void
  canEdit: boolean
}

function useTicketContext() {
  return useOutletContext<TicketContext>()
}

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
  const [deletedTicket, setDeletedTicket] = useState<TicketDetailDto | null>(null)
  const [restoreError, setRestoreError] = useState<string | null>(null)

  const load = useCallback((clear: boolean) => {
    if (clear) {
      setTicket(null)
      setLinks(null)
      setAssociated(null)
      setBacklinks(null)
      setAttachments(null)
      setError(null)
      setEditing(false)
      setDeletedTicket(null)
      setRestoreError(null)
    }
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
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 404) {
          getTicket(ref, [], true)
            .then((t) => setDeletedTicket(t))
            .catch(() => setError(err.message))
          return
        }
        setError(err instanceof ApiError ? err.message : String(err))
      })
  }, [ref])

  function restore() {
    if (!deletedTicket) return
    setRestoreError(null)
    restoreTicket(deletedTicket.ref, deletedTicket.version)
      .then(() => load(true))
      .catch((err: unknown) => setRestoreError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(() => {
    load(true)
  }, [load])

  // Another actor's edit/comment/assignment on this same ticket — a
  // change hint only says "this ref changed," so the response is
  // always a silent refetch, never trusting the hint's own payload
  // (product spec §17, ADR 0020). Suppressed while editing: a
  // background refetch would overwrite `ticket`, bumping the version
  // TicketFieldsForm's useConflictForm keys its draft-reset effect on
  // and silently wiping the user's in-progress edit — staleness here
  // must surface through the PUT's own 409 conflict flow (Phase 4's
  // exit criterion), never a live refetch racing ahead of it.
  useEntityChanged(ticket?.ref, useCallback(() => {
    if (!editing) load(false)
  }, [editing, load]))

  const canEdit = me?.permission === 'editor'

  if (deletedTicket) {
    return (
      <main>
        <h1>
          {deletedTicket.title} <span>({deletedTicket.ref})</span>
        </h1>
        <p role="alert">
          This ticket was deleted
          {deletedTicket.deleted_at ? ` on ${deletedTicket.deleted_at}` : ''}.
          {canEdit && (
            <>
              {' '}
              <button type="button" onClick={restore}>
                Restore
              </button>
            </>
          )}
        </p>
        {restoreError && <p role="alert">{restoreError}</p>}
      </main>
    )
  }

  if (error) return <p role="alert">{error}</p>
  if (!ticket || !links || !associated || !backlinks || !attachments) return <p>Loading ticket…</p>

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

  const linksCount = (ticket.relationships?.length ?? 0) + associated.length + links.length + backlinks.length

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
        {ticket.type} · <StatusChip value={ticket.status} kind="status" /> ·{' '}
        <StatusChip value={ticket.priority} kind="priority" />
        {ticket.severity && (
          <>
            {' '}
            · <StatusChip value={ticket.severity} kind="severity" />
          </>
        )}
      </p>
      <p>
        Assignee: {ticket.assignee ?? 'unassigned'} · Creator: {ticket.creator ?? 'unknown'}
      </p>
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}
      {canEdit && <DeleteTicketButton ticket={ticket} onDeleted={() => load(true)} />}
      <SubscribeButton targetRef={ticket.ref} canEdit={canEdit} />

      {canEdit && (
        <div className="quick-edit">
          <TicketActions ticket={ticket} onUpdated={setTicket} />
        </div>
      )}

      <DetailTabs
        tabs={[
          { to: '.', label: 'Overview', end: true },
          { to: 'links', label: 'Links', count: linksCount },
          { to: 'attachments', label: 'Attachments', count: attachments.length },
        ]}
      />

      <Outlet
        context={
          {
            ticket,
            setTicket,
            links,
            setLinks,
            associated,
            setAssociated,
            backlinks,
            attachments,
            setAttachments,
            canEdit,
          } satisfies TicketContext
        }
      />
    </main>
  )
}

export function TicketOverview() {
  const { ticket, setTicket, canEdit } = useTicketContext()
  return (
    <>
      <section className="detail-section">
        <h2>Description</h2>
        <Markdown projectKey={ticket.project}>{ticket.description}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Comments</h2>
        <CommentsSection
          entityRef={ticket.ref}
          comments={ticket.comments ?? []}
          onChange={(comments) => setTicket({ ...ticket, comments })}
          canEdit={canEdit}
        />
      </section>
    </>
  )
}

export function TicketLinksTab() {
  const { ticket, setTicket, associated, setAssociated, links, setLinks, backlinks, canEdit } =
    useTicketContext()
  return (
    <LinksTabView
      entityRef={ticket.ref}
      relationships={ticket.relationships ?? []}
      onRelationshipsChange={(relationships) => setTicket({ ...ticket, relationships })}
      associated={associated}
      onAssociatedChange={setAssociated}
      links={links}
      onLinksChange={setLinks}
      backlinks={backlinks}
      canEdit={canEdit}
    />
  )
}

export function TicketAttachmentsTab() {
  const { ticket, attachments, setAttachments, canEdit } = useTicketContext()
  return (
    <AttachmentsTabView
      ownerRef={ticket.ref}
      attachments={attachments}
      onChange={setAttachments}
      canEdit={canEdit}
    />
  )
}
