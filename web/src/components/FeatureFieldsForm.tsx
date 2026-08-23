import { getFeature, updateFeature } from '../api/features'
import { useConflictForm } from '../hooks/useConflictForm'
import { ConflictBanner } from './ConflictBanner'
import type { FeatureDetail, Priority } from '../api/types'

const priorities: Priority[] = ['critical', 'high', 'medium', 'low']

interface FeatureFields extends Record<string, string> {
  title: string
  description: string
  priority: string
}

function toFields(f: FeatureDetail): FeatureFields {
  return { title: f.title, description: f.description, priority: f.priority }
}

function fromFields(fields: FeatureFields): Pick<FeatureDetail, 'title' | 'description' | 'priority'> {
  return { title: fields.title, description: fields.description, priority: fields.priority as Priority }
}

/** PATCH /features/{ref} — a full-representation update despite the
 * verb (no partial-update semantics on this endpoint). No status
 * field: features have no independent status transition in this API. */
export function FeatureFieldsForm({
  feature,
  onSaved,
  onCancel,
}: {
  feature: FeatureDetail
  onSaved: (updated: FeatureDetail) => void
  onCancel: () => void
}) {
  const { draft, setField, saving, error, conflict, submit, resolveField, readyToResubmit } =
    useConflictForm<FeatureFields>({
      base: toFields(feature),
      baseVersion: feature.version,
      fetchLive: async () => {
        const live = await getFeature(feature.ref)
        return { fields: toFields(live), version: live.version }
      },
      save: async (fields, expectedVersion) => {
        const updated = await updateFeature(
          feature.ref,
          { title: fields.title, description: fields.description, priority: fields.priority as Priority },
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
            onSaved({ ...feature, ...fromFields(result.fields), version: result.version })
          }
        })
      }}
    >
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
