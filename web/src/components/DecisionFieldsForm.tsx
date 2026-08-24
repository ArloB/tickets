import { getDecision, updateDecision } from '../api/decisions'
import { useConflictForm } from '../hooks/useConflictForm'
import { ConflictBanner } from './ConflictBanner'
import type { DecisionDetail, DecisionStatus } from '../api/types'

const statuses: DecisionStatus[] = ['proposed', 'accepted', 'rejected', 'superseded']

interface DecisionFields extends Record<string, string> {
  title: string
  context: string
  decision: string
  rationale: string
  consequences: string
  status: string
  superseded_by: string
}

function toFields(d: DecisionDetail): DecisionFields {
  return {
    title: d.title,
    context: d.context,
    decision: d.decision,
    rationale: d.rationale,
    consequences: d.consequences,
    status: d.status,
    superseded_by: d.superseded_by ?? '',
  }
}

function fromFields(
  fields: DecisionFields,
): Pick<
  DecisionDetail,
  'title' | 'context' | 'decision' | 'rationale' | 'consequences' | 'status' | 'superseded_by'
> {
  return {
    title: fields.title,
    context: fields.context,
    decision: fields.decision,
    rationale: fields.rationale,
    consequences: fields.consequences,
    status: fields.status as DecisionStatus,
    superseded_by: fields.superseded_by || undefined,
  }
}

/** PATCH /decisions/{ref} — full-representation update with If-Match,
 * same conflict semantics as ticket PUT/feature PATCH (the plan's "no
 * version-compare (none exists yet)" note was stale by the time this
 * was built — see docs/contracts/concurrency.md and updateDecision's
 * doc comment). Status is a plain field here, not a separate PATCH
 * like tickets — decisions have no independent status-transition
 * endpoint, so a status change goes through this same merge.
 * superseded_by clears when left blank, matching every other field
 * here's full-representation contract. */
export function DecisionFieldsForm({
  decision,
  onSaved,
  onCancel,
}: {
  decision: DecisionDetail
  onSaved: (updated: DecisionDetail) => void
  onCancel: () => void
}) {
  const { draft, setField, saving, error, conflict, submit, resolveField, readyToResubmit } =
    useConflictForm<DecisionFields>({
      base: toFields(decision),
      baseVersion: decision.version,
      fetchLive: async () => {
        const live = await getDecision(decision.ref)
        return { fields: toFields(live), version: live.version }
      },
      save: async (fields, expectedVersion) => {
        const updated = await updateDecision(
          decision.ref,
          {
            title: fields.title,
            context: fields.context,
            decision: fields.decision,
            rationale: fields.rationale,
            consequences: fields.consequences,
            status: fields.status as DecisionStatus,
            superseded_by: fields.superseded_by,
          },
          expectedVersion,
        )
        return { fields: toFields(updated), version: updated.version }
      },
    })

  const blocked = conflict !== null && !readyToResubmit

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit().then((result) => {
          if (result) {
            onSaved({ ...decision, ...fromFields(result.fields), version: result.version })
          }
        })
      }}
    >
      <label>
        Title
        <input value={draft.title} onChange={(e) => setField('title', e.target.value)} required />
      </label>
      <label>
        Context
        <textarea value={draft.context} onChange={(e) => setField('context', e.target.value)} rows={5} />
      </label>
      <label>
        Decision
        <textarea
          value={draft.decision}
          onChange={(e) => setField('decision', e.target.value)}
          rows={5}
        />
      </label>
      <label>
        Rationale
        <textarea
          value={draft.rationale}
          onChange={(e) => setField('rationale', e.target.value)}
          rows={5}
        />
      </label>
      <label>
        Consequences
        <textarea
          value={draft.consequences}
          onChange={(e) => setField('consequences', e.target.value)}
          rows={5}
        />
      </label>
      <label>
        Status
        <select value={draft.status} onChange={(e) => setField('status', e.target.value)}>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>
      <label>
        Superseded by
        <input
          value={draft.superseded_by}
          onChange={(e) => setField('superseded_by', e.target.value)}
          placeholder="e.g. ABC-D9"
        />
      </label>

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
