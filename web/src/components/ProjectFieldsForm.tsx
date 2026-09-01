import { getProject, updateProject } from '../api/projects'
import { useConflictForm } from '../hooks/useConflictForm'
import { ConflictBanner } from './ConflictBanner'
import type { ProjectDetail } from '../api/types'

interface ProjectFields extends Record<string, string> {
  title: string
  description: string
}

function toFields(p: ProjectDetail): ProjectFields {
  return { title: p.title, description: p.description }
}

/** PATCH /projects/{key} full-representation update, wired through
 * useConflictForm — the same conflict-resolution machinery
 * TicketFieldsForm/FeatureFieldsForm use. Title/description only; see
 * ProjectArchiveControl for archive/unarchive (ADR 0021), kept as its
 * own action for the same reason status is split from fields
 * elsewhere in this codebase. */
export function ProjectFieldsForm({
  project,
  onSaved,
  onCancel,
}: {
  project: ProjectDetail
  onSaved: (updated: ProjectDetail) => void
  onCancel: () => void
}) {
  const { draft, setField, saving, error, conflict, submit, resolveField, readyToResubmit } =
    useConflictForm<ProjectFields>({
      base: toFields(project),
      baseVersion: project.version,
      fetchLive: async () => {
        const live = await getProject(project.key)
        return { fields: toFields(live), version: live.version }
      },
      save: async (fields, expectedVersion) => {
        const updated = await updateProject(
          project.key,
          { title: fields.title, description: fields.description },
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
            onSaved({ ...project, ...result.fields, version: result.version })
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

      {error && <p role="alert">{error}</p>}
      {conflict && <ConflictBanner conflict={conflict} onResolve={resolveField} />}

      <div className="form-actions">
        <button type="submit" disabled={saving || blocked}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  )
}
