import type { ConflictState } from '../hooks/useConflictForm'

const fieldLabels: Record<string, string> = {
  type: 'Type',
  title: 'Title',
  description: 'Description',
  priority: 'Priority',
  severity: 'Severity',
  status: 'Status',
  context: 'Context',
  decision: 'Decision',
  rationale: 'Rationale',
}

/** Renders the plan.md §3 conflict-resolution UI for one edit form: a
 * per-field base/server/draft choice for every field both sides
 * changed differently, plus a note about fields that were silently
 * updated to the server's value because the user never touched them.
 * Nothing here discards the draft — every choice writes back into the
 * form via `onResolve`, and the caller's Save button stays disabled
 * until every conflict is resolved (useConflictForm's
 * `readyToResubmit`). */
export function ConflictBanner({
  conflict,
  onResolve,
}: {
  conflict: ConflictState
  onResolve: (field: string, value: string) => void
}) {
  return (
    <div role="alert" className="conflict-banner">
      <p>
        Someone else changed this record while you were editing (now at version{' '}
        {conflict.serverVersion}).
        {conflict.autoApplied.length > 0 && (
          <>
            {' '}
            Fields you didn't touch (
            {conflict.autoApplied.map((f) => fieldLabels[f] ?? f).join(', ')}) were updated to
            their current value automatically.
          </>
        )}
      </p>
      {conflict.fields.length === 0 ? (
        <p>All conflicts resolved — click Save to submit.</p>
      ) : (
        conflict.fields.map((f) => (
          <fieldset key={f.field}>
            <legend>{fieldLabels[f.field] ?? f.field}</legend>
            <p>Original: {f.base || '(empty)'}</p>
            <label>
              <input
                type="radio"
                name={`resolve-${f.field}`}
                onChange={() => onResolve(f.field, f.server)}
              />
              Their change: {f.server || '(empty)'}
            </label>
            <label>
              <input type="radio" name={`resolve-${f.field}`} onChange={() => onResolve(f.field, f.draft)} />
              Your change: {f.draft || '(empty)'}
            </label>
          </fieldset>
        ))
      )}
    </div>
  )
}
