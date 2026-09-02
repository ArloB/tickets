import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { listTickets, reorderTicket, updateTicketStatus } from '../api/tickets'
import { ApiError } from '../api/client'
import { useProjectChanged } from '../api/events'
import { useAuth } from '../auth/AuthContext'
import { StatusChip } from '../components/StatusChip'
import { useBoardDrag } from '../hooks/useBoardDrag'
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
 * 100k-ticket reference dataset. */
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
  // A card moving to a new status column unmounts its <select> from
  // one column's list and mounts a fresh one in another — different
  // parent lists, so React can't preserve focus across the move the
  // way key-based reconciliation does within one list. This
  // announcement is the fallback for a screen-reader user once focus
  // silently drops to the document body.
  const [moveAnnouncement, setMoveAnnouncement] = useState('')

  function currentTicket(ref: string): TicketCompact | undefined {
    for (const status of statuses) {
      const found = columns[status].tickets?.find((t) => t.ref === ref)
      if (found) return found
    }
    return undefined
  }

  const reload = useCallback(
    (clear: boolean) => {
      if (clear) {
        setColumns(
          Object.fromEntries(statuses.map((s) => [s, { tickets: null, error: null }])) as Record<
            WorkflowStatus,
            ColumnState
          >,
        )
      }
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
    },
    [key],
  )

  useEffect(() => {
    reload(true)
  }, [reload])

  // Another browser's move/create/edit in this project — reload every
  // column rather than trying to patch just the affected card, since
  // the hint doesn't say which column it landed in (product spec §17).
  useProjectChanged(key, useCallback(() => reload(false), [reload]))

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

  async function moveCard(dragged: TicketCompact, newStatus: WorkflowStatus) {
    const ticket = currentTicket(dragged.ref) ?? dragged
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
      setMoveAnnouncement(`Moved ${ticket.ref} to ${newStatus}`)
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

  async function reorderCard(dragged: TicketCompact, afterRef: string | null) {
    const ticket = currentTicket(dragged.ref) ?? dragged
    try {
      await reorderTicket(ticket.ref, afterRef, ticket.version)
      const page = await listTickets(
        key,
        'priority_queue',
        { status: ticket.status },
        undefined,
        COLUMN_PAGE_SIZE,
      )
      setColumns((prev) => ({
        ...prev,
        [ticket.status]: { tickets: page.tickets, nextCursor: page.next_cursor, error: null },
      }))
      setMoveAnnouncement(`Moved ${ticket.ref}`)
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

  const { dragging, dragProps, columnDropProps, cardDropProps } = useBoardDrag<TicketCompact>(
    (t, afterRef) => void reorderCard(t, afterRef),
    (t, status) => void moveCard(t, status),
  )

  return (
    <main>
      <h1>Board — {key}</h1>
      <p aria-live="polite" className="sr-only">
        {moveAnnouncement}
      </p>
      <div className="board">
        {statuses.map((status) => {
          const col = columns[status]
          const tickets = col.tickets
          return (
            <section key={status} className="board-column">
              <h2>
                <StatusChip value={status} kind="status" />
              </h2>
              {col.error && <p role="alert">{col.error}</p>}
              {!tickets ? (
                <p>Loading…</p>
              ) : (
                <div className="board-column-list" {...(canEdit ? columnDropProps(status) : {})}>
                  <ul>
                    {tickets.map((t) => (
                      <li
                        key={t.ref}
                        className={
                          dragging?.ref === t.ref ? 'board-card board-card-dragging' : 'board-card'
                        }
                        {...(canEdit ? dragProps(t) : {})}
                        {...(canEdit ? cardDropProps(t, tickets) : {})}
                      >
                        <Link to={`/tickets/${t.ref}`}>{t.ref}</Link>
                        <p>{t.title}</p>
                        <p>
                          {t.type} · <StatusChip value={t.priority} kind="priority" />
                          {t.severity ? (
                            <>
                              {' '}
                              · <StatusChip value={t.severity} kind="severity" />
                            </>
                          ) : (
                            ''
                          )}
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
                </div>
              )}
              {col.nextCursor && <button onClick={() => void loadMore(status)}>Load more</button>}
            </section>
          )
        })}
      </div>
    </main>
  )
}
