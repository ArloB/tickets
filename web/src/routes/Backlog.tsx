import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import {
  createTicket,
  getTicket,
  listTickets,
  reorderTicket,
  updateTicketStatus,
  type TicketListFilters,
  type TicketListView,
} from '../api/tickets'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
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

function NewTicketForm({
  projectKey,
  onCreated,
}: {
  projectKey: string
  onCreated: (t: TicketCompact) => void
}) {
  const [type, setType] = useState<TicketType>('task')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState<Priority>('medium')
  const [severity, setSeverity] = useState<Severity | ''>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const severityApplicable = type === 'bug' || type === 'security'

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createTicket(projectKey, {
        type,
        title,
        description,
        priority,
        severity: severityApplicable && severity !== '' ? severity : null,
      })
      onCreated({
        ref: created.ref,
        title: created.title,
        type: created.type,
        status: created.status,
        priority: created.priority,
        severity: created.severity,
        version: created.version,
        updated_at: created.updated_at,
      })
      setTitle('')
      setDescription('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <label>
        Type
        <select value={type} onChange={(e) => setType(e.target.value as TicketType)}>
          {types.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </label>
      <label>
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Description
        <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={4} />
      </label>
      <label>
        Priority
        <select value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
          {priorities.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </label>
      {severityApplicable && (
        <label>
          Severity
          <select value={severity} onChange={(e) => setSeverity(e.target.value as Severity | '')}>
            <option value="">None</option>
            {severities.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
      )}
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create ticket'}
      </button>
    </form>
  )
}

interface BulkResult {
  ref: string
  ok: boolean
  message?: string
}

function BulkActions({
  selected,
  tickets,
  onUpdated,
  onClearSelection,
  results,
  onResults,
}: {
  selected: Set<string>
  tickets: TicketCompact[]
  onUpdated: (updated: TicketCompact) => void
  onClearSelection: (refs: string[]) => void
  results: BulkResult[] | null
  // Lifted to the parent rather than kept as this component's own
  // state: a fully-successful apply clears every succeeded ref from
  // `selected` (see onClearSelection below), which would otherwise
  // drop `selected.size` to 0 and unmount this component — in the
  // very same render as the results it just produced — before the
  // "done"/"failed" list ever reached the DOM. The parent keeps this
  // component mounted whenever there's a result to show, regardless
  // of what's still selected.
  onResults: (results: BulkResult[] | null) => void
}) {
  const [bulkStatus, setBulkStatus] = useState<WorkflowStatus>('backlog')
  const [running, setRunning] = useState(false)

  async function apply() {
    setRunning(true)
    onResults(null)
    const targets = tickets.filter((t) => selected.has(t.ref))
    const outcomes: BulkResult[] = []
    const succeededRefs: string[] = []
    // No bulk endpoint exists — N sequential requests, each with its
    // own If-Match from that row's cached version. Partial failure is
    // the normal case here (some rows conflict, most don't), so every
    // row gets its own result rather than one all-or-nothing banner.
    for (const t of targets) {
      try {
        const updated = await updateTicketStatus(t.ref, bulkStatus, t.version)
        onUpdated({
          ref: updated.ref,
          title: updated.title,
          type: updated.type,
          status: updated.status,
          priority: updated.priority,
          severity: updated.severity,
          version: updated.version,
          updated_at: updated.updated_at,
        })
        outcomes.push({ ref: t.ref, ok: true })
        succeededRefs.push(t.ref)
      } catch (err) {
        outcomes.push({
          ref: t.ref,
          ok: false,
          message: err instanceof ApiError ? err.message : String(err),
        })
        // A failed row stays selected for retry, but retrying with the
        // same stale version would just 409 again forever — refresh
        // the row's cached version (and any other fields someone else
        // changed) from the server so a second "Apply" attempt has a
        // real shot at succeeding.
        if (err instanceof ApiError && err.code === 'version_conflict') {
          try {
            const live = await getTicket(t.ref)
            onUpdated({
              ref: live.ref,
              title: live.title,
              type: live.type,
              status: live.status,
              priority: live.priority,
              severity: live.severity,
              version: live.version,
              updated_at: live.updated_at,
            })
          } catch {
            // Best-effort refresh — if this also fails, the row is
            // still selected and the visible error still explains why.
          }
        }
      }
    }
    onResults(outcomes)
    onClearSelection(succeededRefs)
    setRunning(false)
  }

  return (
    <div>
      <p>{selected.size} selected</p>
      <label>
        Set status to
        <select value={bulkStatus} onChange={(e) => setBulkStatus(e.target.value as WorkflowStatus)}>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>
      <button type="button" disabled={running || selected.size === 0} onClick={() => void apply()}>
        {running ? 'Applying…' : 'Apply to selected'}
      </button>
      {results && (
        <ul>
          {results.map((r) => (
            <li key={r.ref}>
              {r.ref}: {r.ok ? 'done' : `failed — ${r.message}`}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** Backlog list — reuses the priority_queue ordering with filters
 * layered on top, per the Phase 4 plan's note that there's no
 * separate third ?view= value for a plain backlog. Reorder (drag
 * substitute: Up/Down buttons, keyboard-operable) is only offered in
 * priority_queue view, since position is meaningless for
 * issue_register's severity ordering. */
export default function Backlog() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [params, setParams] = useSearchParams()
  const [tickets, setTickets] = useState<TicketCompact[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [bulkResults, setBulkResults] = useState<BulkResult[] | null>(null)
  const [reorderError, setReorderError] = useState<string | null>(null)
  const [moveAnnouncement, setMoveAnnouncement] = useState('')

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
  const canEdit = me?.permission === 'editor'
  const canReorder = canEdit && view === 'priority_queue'

  // Re-fetches (replacing the list, not appending) whenever the filter/
  // view URL params change — "Load more" below is the only path that
  // appends to an existing list.
  useEffect(() => {
    setTickets(null)
    setError(null)
    setSelected(new Set())
    setBulkResults(null)
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

  function toggleSelected(ref: string) {
    // A fresh checkbox change means the user is building a new batch,
    // not still reviewing the last one — drop any stale results so
    // they don't linger next to an unrelated selection.
    setBulkResults(null)
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(ref)) next.delete(ref)
      else next.add(ref)
      return next
    })
  }

  // Patches the row in place without re-sorting or refetching — safe
  // only because priority_queue order doesn't depend on status, so a
  // status-only bulk change can't move a row out of its slot. Revisit
  // this if a status-ordered view is ever added.
  function applyBulkUpdate(updated: TicketCompact) {
    setTickets((prev) => (prev ? prev.map((t) => (t.ref === updated.ref ? updated : t)) : prev))
  }

  function clearSelection(refs: string[]) {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const ref of refs) next.delete(ref)
      return next
    })
  }

  // Swaps ticket at `index` with its neighbor at `index + direction`
  // (direction: -1 for up, +1 for down). Reorder is only valid within
  // the same priority band (api/openapi.yaml's ReorderRequest doc
  // comment) — crossing bands here would need a priority change
  // first, so that neighbor is simply not offered.
  //
  // afterRef is derived from adjacency in the *currently loaded,
  // possibly filtered* list, which doesn't necessarily match the
  // server's true band adjacency (a filter can hide the real
  // predecessor). So after a successful reorder we don't trust an
  // optimistic local swap — we refetch page one of the current
  // view/filters and let the server's order win. This does lose any
  // extra pages pulled in via "Load more," which is an acceptable
  // cost for a rare action.
  async function move(index: number, direction: -1 | 1) {
    if (!tickets) return
    const neighborIndex = index + direction
    if (neighborIndex < 0 || neighborIndex >= tickets.length) return
    const ticket = tickets[index]
    const neighbor = tickets[neighborIndex]
    if (ticket.priority !== neighbor.priority) return

    setReorderError(null)
    const afterRef =
      direction === 1
        ? neighbor.ref
        : (tickets[neighborIndex - 1]?.priority === ticket.priority
            ? tickets[neighborIndex - 1].ref
            : null)
    try {
      await reorderTicket(ticket.ref, afterRef, ticket.version)
      const page = await listTickets(key, view, filters)
      setTickets(page.tickets)
      setNextCursor(page.next_cursor)
      // The refetch swaps in fresh row objects, and moving to the head
      // of a priority band disables the button that triggered this
      // (i === 0 now) — the browser drops focus off a disabled button
      // with nothing else to say what happened. An aria-live
      // announcement is the standard fallback for a screen-reader user
      // when the visual move itself is the only other feedback.
      setMoveAnnouncement(`Moved ${ticket.ref} ${direction === -1 ? 'up' : 'down'}`)
    } catch (err) {
      setReorderError(err instanceof ApiError ? err.message : String(err))
    }
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

      <p aria-live="polite" className="sr-only">
        {moveAnnouncement}
      </p>
      {error && <p role="alert">{error}</p>}
      {reorderError && <p role="alert">{reorderError}</p>}
      {!error && !tickets && <p>Loading tickets…</p>}
      {tickets && tickets.length === 0 && <p>No tickets match these filters.</p>}
      {tickets && tickets.length > 0 && (
        <table>
          <thead>
            <tr>
              {canEdit && <th>Select</th>}
              <th>Ref</th>
              <th>Title</th>
              <th>Type</th>
              <th>Status</th>
              <th>Priority</th>
              <th>Severity</th>
              {canReorder && <th>Reorder</th>}
            </tr>
          </thead>
          <tbody>
            {tickets.map((t, i) => (
              <tr key={t.ref}>
                {canEdit && (
                  <td>
                    <input
                      type="checkbox"
                      checked={selected.has(t.ref)}
                      onChange={() => toggleSelected(t.ref)}
                      aria-label={`Select ${t.ref}`}
                    />
                  </td>
                )}
                <td>
                  <Link to={`/tickets/${t.ref}`}>{t.ref}</Link>
                </td>
                <td>{t.title}</td>
                <td>{t.type}</td>
                <td>{t.status}</td>
                <td>{t.priority}</td>
                <td>{t.severity ?? ''}</td>
                {canReorder && (
                  <td>
                    <button
                      type="button"
                      onClick={() => void move(i, -1)}
                      disabled={i === 0 || tickets[i - 1].priority !== t.priority}
                      aria-label={`Move ${t.ref} up`}
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      onClick={() => void move(i, 1)}
                      disabled={i === tickets.length - 1 || tickets[i + 1].priority !== t.priority}
                      aria-label={`Move ${t.ref} down`}
                    >
                      ↓
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {nextCursor && <button onClick={loadMore}>Load more</button>}

      {canEdit && (selected.size > 0 || bulkResults !== null) && tickets && (
        <BulkActions
          selected={selected}
          tickets={tickets}
          onUpdated={applyBulkUpdate}
          onClearSelection={clearSelection}
          results={bulkResults}
          onResults={setBulkResults}
        />
      )}

      {me?.permission === 'editor' &&
        (creating ? (
          <NewTicketForm
            projectKey={key}
            onCreated={(t) => {
              setTickets([...(tickets ?? []), t])
              setCreating(false)
            }}
          />
        ) : (
          <button onClick={() => setCreating(true)}>New ticket</button>
        ))}
    </main>
  )
}
