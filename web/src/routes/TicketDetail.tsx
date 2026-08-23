import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getTicket,
  listTicketBacklinks,
  listTicketLinks,
} from '../api/tickets'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import type { Backlink, ExternalLink, TicketDetail as TicketDetailDto } from '../api/types'

export default function TicketDetail() {
  const { ref = '' } = useParams()
  const [ticket, setTicket] = useState<TicketDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setTicket(null)
    setLinks(null)
    setBacklinks(null)
    setError(null)
    Promise.all([
      getTicket(ref, ['comments', 'relationships']),
      listTicketLinks(ref),
      listTicketBacklinks(ref),
    ])
      .then(([t, l, b]) => {
        setTicket(t)
        setLinks(l.links)
        setBacklinks(b.backlinks)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  if (error) return <p role="alert">{error}</p>
  if (!ticket) return <p>Loading ticket…</p>

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

      <h2>Description</h2>
      <Markdown>{ticket.description}</Markdown>

      <h2>Relationships</h2>
      {!ticket.relationships || ticket.relationships.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {ticket.relationships.map((r) => (
            <li key={`${r.type}-${r.other}`}>
              {r.type} <Link to={refPath(r.other)}>{r.other}</Link>
            </li>
          ))}
        </ul>
      )}

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
              <Link to={refPath(b.ref)}>{b.ref}</Link>
              {b.comment_id !== undefined ? ` (comment #${b.comment_id})` : ' (description)'}
            </li>
          ))}
        </ul>
      )}

      <h2>Comments</h2>
      {!ticket.comments || ticket.comments.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {ticket.comments.map((c) => (
            <li key={c.id}>
              <p>
                <strong>{c.author}</strong> — {c.created_at}
                {c.deleted_at ? ' (deleted)' : ''}
              </p>
              <Markdown>{c.body}</Markdown>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}

/** Relationship/backlink targets are bare refs (e.g. "ABC-123",
 * "ABC-F1") — route by kind so a link goes to the right detail view.
 * Decisions aren't a Milestone 2 route yet, so a decision ref falls
 * back to its project overview rather than a dead link. */
function refPath(ref: string): string {
  if (/-F\d+$/.test(ref)) return `/features/${ref}`
  if (/-D\d+$/.test(ref)) return `/projects/${ref.split('-')[0]}`
  return `/tickets/${ref}`
}
