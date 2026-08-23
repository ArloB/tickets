import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { listTickets, updateTicketStatus } from '../api/tickets'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import type { TicketCompact, WorkflowStatus } from '../api/types'

const statuses: WorkflowStatus[] = [
  'backlog',
  'ready',
  'in_progress',
  'blocked',
  'review',
  'done',
  'cancelled',
]

const COLUMN_PAGE_SIZE = 50

interface ColumnState {
  tickets: TicketCompact[] | null
  nextCursor?: string
  error: string | null
}

/** Grouped-by-status board. Each column is its own filtered/paginated
 * fetch (the Milestone 1 `status` filter) rather than one unfiltered
 * fetch grouped client-side — the latter doesn't hold up at §11's
 * 100k-ticket reference dataset. Status changes move a card between
 * columns via a <select>, not pointer drag-and-drop: this keeps the
 * board fully keyboard-operable without a separate a11y pass
 * (Milestone 5's plan flags HTML5 native DnD as having no keyboard
 * path at all). */
export default function TicketBoard() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [columns, setColumns] = useState<Record<WorkflowStatus, ColumnState>>(
    () =>
      Object.fromEntries(statuses.map((s) => [s, { tickets: null, error: null }])) as Record<
        WorkflowStatus,
        ColumnState
      >,
  )
  const canEdit = me?.permission === 'editor'

  useEffect(() => {
    setColumns(
      Object.fromEntries(statuses.map((s) => [s, { tickets: null, error: null }])) as Record<
        WorkflowStatus,
        ColumnState
      >,
    )
    for (const status of statuses) {
      listTickets(key, 'priority_queue', { status }, undefined, COLUMN_PAGE_SIZE)
        .then((page) => {
          setColumns((prev) => ({
            ...prev,
            [status]: { tickets: page.tickets, nextCursor: page.next_cursor, error: null },
          }))
        })
        .catch((err: unknown) => {
          setColumns((prev) => ({
            ...prev,
            [status]: {
              tickets: [],
              error: err instanceof ApiError ? err.message : String(err),
            },
          }))
        })
    }
  }, [key])

  async function loadMore(status: WorkflowStatus) {
    const col = columns[status]
    if (!col.nextCursor) return
    try {
      const page = await listTickets(
        key,
        'priority_queue',
        { status },
        col.nextCursor,
        COLUMN_PAGE_SIZE,
      )
      setColumns((prev) => ({
        ...prev,
        [status]: {
          tickets: [...(prev[status].tickets ?? []), ...page.tickets],
          nextCursor: page.next_cursor,
          error: null,
        },
      }))
    } catch (err) {
      setColumns((prev) => ({
        ...prev,
        [status]: { ...prev[status], error: err instanceof ApiError ? err.message : String(err) },
      }))
    }
  }

  async function moveCard(ticket: TicketCompact, newStatus: WorkflowStatus) {
    if (newStatus === ticket.status) return
    try {
      const updated = await updateTicketStatus(ticket.ref, newStatus, ticket.version)
      setColumns((prev) => {
        const next = { ...prev }
        next[ticket.status] = {
          ...prev[ticket.status],
          tickets: (prev[ticket.status].tickets ?? []).filter((t) => t.ref !== ticket.ref),
        }
        const moved: TicketCompact = {
          ref: updated.ref,
          title: updated.title,
          type: updated.type,
          status: updated.status,
          priority: updated.priority,
          severity: updated.severity,
          version: updated.version,
          updated_at: updated.updated_at,
        }
        next[newStatus] = {
          ...prev[newStatus],
          tickets: [moved, ...(prev[newStatus].tickets ?? [])],
        }
        return next
      })
    } catch (err) {
      setColumns((prev) => ({
        ...prev,
        [ticket.status]: {
          ...prev[ticket.status],
          error: err instanceof ApiError ? err.message : String(err),
        },
      }))
    }
  }

  return (
    <main>
      <h1>Board — {key}</h1>
      <div className="board">
        {statuses.map((status) => {
          const col = columns[status]
          return (
            <section key={status} className="board-column">
              <h2>{status}</h2>
              {col.error && <p role="alert">{col.error}</p>}
              {!col.tickets ? (
                <p>Loading…</p>
              ) : (
                <ul>
                  {col.tickets.map((t) => (
                    <li key={t.ref} className="board-card">
                      <Link to={`/tickets/${t.ref}`}>{t.ref}</Link>
                      <p>{t.title}</p>
                      <p>
                        {t.type} · {t.priority}
                        {t.severity ? ` · ${t.severity}` : ''}
                      </p>
                      {canEdit && (
                        <label>
                          Move to
                          <select
                            value={status}
                            onChange={(e) => void moveCard(t, e.target.value as WorkflowStatus)}
                          >
                            {statuses.map((s) => (
                              <option key={s} value={s}>
                                {s}
                              </option>
                            ))}
                          </select>
                        </label>
                      )}
                    </li>
                  ))}
                </ul>
              )}
              {col.nextCursor && <button onClick={() => void loadMore(status)}>Load more</button>}
            </section>
          )
        })}
      </div>
    </main>
  )
}
