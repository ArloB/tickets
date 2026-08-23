import { useState } from 'react'
import { assignTicket, getTicket, moveTicketFeature, updateTicketStatus } from '../api/tickets'
import { ApiError } from '../api/client'
import type { TicketDetail, WorkflowStatus } from '../api/types'

const statuses: WorkflowStatus[] = [
  'backlog',
  'ready',
  'in_progress',
  'blocked',
  'review',
  'done',
  'cancelled',
]

/** Status/assign/move are each single-field mutations with their own
 * If-Match — a version conflict here doesn't need the ticket edit
 * form's per-field merge (plan.md §3 step 5: "Status-only PATCH
 * conflicts need only 'someone changed status to X, you wanted Y —
 * apply anyway?' with no multi-field merge"). All three bump
 * ticket.version and return the full updated ticket, so `onUpdated`
 * replaces the caller's cached ticket wholesale to keep it in sync. */
export function TicketActions({
  ticket,
  onUpdated,
}: {
  ticket: TicketDetail
  onUpdated: (updated: TicketDetail) => void
}) {
  return (
    <>
      <StatusControl ticket={ticket} onUpdated={onUpdated} />
      <AssignControl ticket={ticket} onUpdated={onUpdated} />
      <MoveControl ticket={ticket} onUpdated={onUpdated} />
    </>
  )
}

function StatusControl({
  ticket,
  onUpdated,
}: {
  ticket: TicketDetail
  onUpdated: (updated: TicketDetail) => void
}) {
  const [value, setValue] = useState<WorkflowStatus>(ticket.status)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<{ liveStatus: WorkflowStatus; version: number } | null>(
    null,
  )

  async function apply(expectedVersion: number) {
    setSaving(true)
    setError(null)
    try {
      const updated = await updateTicketStatus(ticket.ref, value, expectedVersion)
      onUpdated(updated)
      setConflict(null)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        const live = await getTicket(ticket.ref)
        setConflict({ liveStatus: live.status, version: live.version })
      } else {
        setError(err instanceof ApiError ? err.message : String(err))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <label>
        Status
        <select value={value} onChange={(e) => setValue(e.target.value as WorkflowStatus)}>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>
      <button
        type="button"
        disabled={saving || value === ticket.status}
        onClick={() => void apply(ticket.version)}
      >
        Update status
      </button>
      {conflict && (
        <p role="alert">
          Someone changed the status to "{conflict.liveStatus}" (version {conflict.version}) since
          this page loaded.{' '}
          <button type="button" onClick={() => void apply(conflict.version)}>
            Apply "{value}" anyway
          </button>
        </p>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  )
}

function AssignControl({
  ticket,
  onUpdated,
}: {
  ticket: TicketDetail
  onUpdated: (updated: TicketDetail) => void
}) {
  const [value, setValue] = useState(ticket.assignee ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<{ liveAssignee: string | null; version: number } | null>(
    null,
  )

  async function apply(expectedVersion: number) {
    setSaving(true)
    setError(null)
    try {
      const updated = await assignTicket(ticket.ref, value === '' ? null : value, expectedVersion)
      onUpdated(updated)
      setConflict(null)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        const live = await getTicket(ticket.ref)
        setConflict({ liveAssignee: live.assignee ?? null, version: live.version })
      } else {
        setError(err instanceof ApiError ? err.message : String(err))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <label>
        Assignee
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="human:alice"
        />
      </label>
      <button
        type="button"
        disabled={saving || value === (ticket.assignee ?? '')}
        onClick={() => void apply(ticket.version)}
      >
        {saving ? 'Saving…' : 'Update assignee'}
      </button>
      {conflict && (
        <p role="alert">
          Someone set the assignee to "{conflict.liveAssignee ?? 'unassigned'}" (version{' '}
          {conflict.version}) since this page loaded.{' '}
          <button type="button" onClick={() => void apply(conflict.version)}>
            Apply "{value || 'unassigned'}" anyway
          </button>
        </p>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  )
}

function MoveControl({
  ticket,
  onUpdated,
}: {
  ticket: TicketDetail
  onUpdated: (updated: TicketDetail) => void
}) {
  const [value, setValue] = useState(ticket.feature)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<{ liveFeature: string; version: number } | null>(null)

  async function apply(expectedVersion: number) {
    setSaving(true)
    setError(null)
    try {
      const updated = await moveTicketFeature(ticket.ref, value, expectedVersion)
      onUpdated(updated)
      setConflict(null)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        const live = await getTicket(ticket.ref)
        setConflict({ liveFeature: live.feature, version: live.version })
      } else {
        setError(err instanceof ApiError ? err.message : String(err))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <label>
        Feature
        <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="ABC-F1" />
      </label>
      <button
        type="button"
        disabled={saving || value === ticket.feature}
        onClick={() => void apply(ticket.version)}
      >
        {saving ? 'Moving…' : 'Move to feature'}
      </button>
      {conflict && (
        <p role="alert">
          Someone moved this ticket to "{conflict.liveFeature}" (version {conflict.version}) since
          this page loaded.{' '}
          <button type="button" onClick={() => void apply(conflict.version)}>
            Move to "{value}" anyway
          </button>
        </p>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  )
}
