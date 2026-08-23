import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { listTickets, type TicketListFilters, type TicketListView } from '../api/tickets'
import { ApiError } from '../api/client'
import type { Priority, Severity, TicketCompact, TicketType, WorkflowStatus } from '../api/types'

const statuses: WorkflowStatus[] = [
  'backlog',
  'ready',
  'in_progress',
  'blocked',
  'review',
  'done',
  'cancelled',
]
const types: TicketType[] = ['task', 'bug', 'security', 'chore']
const severities: Severity[] = ['critical', 'high', 'medium', 'low']
const priorities: Priority[] = ['critical', 'high', 'medium', 'low']

/** Read-only backlog list for Milestone 2 — reuses the priority_queue
 * ordering with filters layered on top, per the Phase 4 plan's note
 * that there's no separate third ?view= value for a plain backlog.
 * Bulk selection is Milestone 4. */
export default function Backlog() {
  const { key = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const [tickets, setTickets] = useState<TicketCompact[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [error, setError] = useState<string | null>(null)

  const view = (params.get('view') as TicketListView | null) ?? 'priority_queue'
  const filters: TicketListFilters = {
    status: (params.get('status') as WorkflowStatus | null) ?? undefined,
    type: (params.get('type') as TicketType | null) ?? undefined,
    severity: (params.get('severity') as Severity | null) ?? undefined,
    priority: (params.get('priority') as Priority | null) ?? undefined,
    featureRef: params.get('feature_ref') ?? undefined,
    assignee: params.get('assignee') ?? undefined,
    creator: params.get('creator') ?? undefined,
    updatedSince: params.get('updated_since') ?? undefined,
  }

  // Re-fetches (replacing the list, not appending) whenever the filter/
  // view URL params change — "Load more" below is the only path that
  // appends to an existing list.
  useEffect(() => {
    setTickets(null)
    setError(null)
    listTickets(key, view, filters)
      .then((page) => {
        setTickets(page.tickets)
        setNextCursor(page.next_cursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
    // filters/view are derived from `params` each render; re-fetching on
    // the raw search-string change avoids a stale-closure dependency list.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, params.toString()])

  function setFilter(name: string, value: string) {
    const next = new URLSearchParams(params)
    if (value) {
      next.set(name, value)
    } else {
      next.delete(name)
    }
    setParams(next)
  }

  function loadMore() {
    if (!nextCursor) return
    listTickets(key, view, filters, nextCursor)
      .then((page) => {
        setTickets((prev) => [...(prev ?? []), ...page.tickets])
        setNextCursor(page.next_cursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  return (
    <main>
      <h1>Backlog — {key}</h1>
      <form>
        <label>
          View
          <select value={view} onChange={(e) => setFilter('view', e.target.value)}>
            <option value="priority_queue">Priority queue</option>
            <option value="issue_register">Issue register</option>
          </select>
        </label>
        <label>
          Status
          <select value={filters.status ?? ''} onChange={(e) => setFilter('status', e.target.value)}>
            <option value="">Any</option>
            {statuses.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
        <label>
          Type
          <select value={filters.type ?? ''} onChange={(e) => setFilter('type', e.target.value)}>
            <option value="">Any</option>
            {types.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>
        <label>
          Severity
          <select
            value={filters.severity ?? ''}
            onChange={(e) => setFilter('severity', e.target.value)}
          >
            <option value="">Any</option>
            {severities.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
        <label>
          Priority
          <select
            value={filters.priority ?? ''}
            onChange={(e) => setFilter('priority', e.target.value)}
          >
            <option value="">Any</option>
            {priorities.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </label>
        <label>
          Assignee
          <input
            value={filters.assignee ?? ''}
            onChange={(e) => setFilter('assignee', e.target.value)}
          />
        </label>
        <label>
          Creator
          <input
            value={filters.creator ?? ''}
            onChange={(e) => setFilter('creator', e.target.value)}
          />
        </label>
      </form>

      {error && <p role="alert">{error}</p>}
      {!error && !tickets && <p>Loading tickets…</p>}
      {tickets && tickets.length === 0 && <p>No tickets match these filters.</p>}
      {tickets && tickets.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Ref</th>
              <th>Title</th>
              <th>Type</th>
              <th>Status</th>
              <th>Priority</th>
              <th>Severity</th>
            </tr>
          </thead>
          <tbody>
            {tickets.map((t) => (
              <tr key={t.ref}>
                <td>
                  <Link to={`/tickets/${t.ref}`}>{t.ref}</Link>
                </td>
                <td>{t.title}</td>
                <td>{t.type}</td>
                <td>{t.status}</td>
                <td>{t.priority}</td>
                <td>{t.severity ?? ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {nextCursor && <button onClick={loadMore}>Load more</button>}
    </main>
  )
}
