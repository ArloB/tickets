import { getTicket, updateTicketFields } from '../api/tickets'
import { useConflictForm } from '../hooks/useConflictForm'
import { ConflictBanner } from './ConflictBanner'
import type { Priority, Severity, TicketDetail, TicketType } from '../api/types'

const types: TicketType[] = ['task', 'bug', 'security', 'chore']
const priorities: Priority[] = ['critical', 'high', 'medium', 'low']
const severities: Severity[] = ['critical', 'high', 'medium', 'low']

interface TicketFields extends Record<string, string> {
  type: string
  title: string
  description: string
  priority: string
  severity: string
}

function toFields(t: TicketDetail): TicketFields {
  return {
    type: t.type,
    title: t.title,
    description: t.description,
    priority: t.priority,
    severity: t.severity ?? '',
  }
}

function fromFields(fields: TicketFields): Pick<TicketDetail, 'type' | 'title' | 'description' | 'priority' | 'severity'> {
  return {
    type: fields.type as TicketType,
    title: fields.title,
    description: fields.description,
    priority: fields.priority as Priority,
    severity: fields.severity === '' ? undefined : (fields.severity as Severity),
  }
}

/** PUT /tickets/{ref} full-representation update, wired through
 * useConflictForm — the first (and reference) instance of the Phase 4
 * exit criterion's conflict-resolution UI, built once and reused by
 * FeatureFieldsForm/DecisionFieldsForm. */
export function TicketFieldsForm({
  ticket,
  onSaved,
  onCancel,
}: {
  ticket: TicketDetail
  onSaved: (updated: TicketDetail) => void
  onCancel: () => void
}) {
  const { draft, setField, saving, error, conflict, submit, resolveField, readyToResubmit } =
    useConflictForm<TicketFields>({
      base: toFields(ticket),
      baseVersion: ticket.version,
      fetchLive: async () => {
        const live = await getTicket(ticket.ref)
        return { fields: toFields(live), version: live.version }
      },
      save: async (fields, expectedVersion) => {
        const updated = await updateTicketFields(
          ticket.ref,
          {
            type: fields.type as TicketType,
            title: fields.title,
            description: fields.description,
            priority: fields.priority as Priority,
            severity: fields.severity === '' ? null : (fields.severity as Severity),
          },
          expectedVersion,
        )
        return { fields: toFields(updated), version: updated.version }
      },
    })

  const severityApplicable = draft.type === 'bug' || draft.type === 'security'
  const blocked = conflict !== null && !readyToResubmit

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit().then((result) => {
          // Only fire once the hook has fully settled (including any
          // no-decision-needed retry) — never from inside `save`
          // itself, since the caller's onSaved (TicketDetail) unmounts
          // this form, and useConflictForm still has state updates
          // pending after a successful save.
          if (result) {
            onSaved({ ...ticket, ...fromFields(result.fields), version: result.version })
          }
        })
      }}
    >
      <label>
        Type
        <select value={draft.type} onChange={(e) => setField('type', e.target.value)}>
          {types.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </label>
      <label>
        Title
        <input value={draft.title} onChange={(e) => setField('title', e.target.value)} required />
      </label>
      <label>
        Description
        <textarea
          value={draft.description}
          onChange={(e) => setField('description', e.target.value)}
          rows={8}
        />
      </label>
      <label>
        Priority
        <select value={draft.priority} onChange={(e) => setField('priority', e.target.value)}>
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
          <select value={draft.severity} onChange={(e) => setField('severity', e.target.value)}>
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
      {conflict && <ConflictBanner conflict={conflict} onResolve={resolveField} />}

      <button type="submit" disabled={saving || blocked}>
        {saving ? 'Saving…' : 'Save'}
      </button>
      <button type="button" onClick={onCancel}>
        Cancel
      </button>
    </form>
  )
}
