import { useState } from 'react'
import { Link } from 'react-router-dom'
import { addRelationship, removeRelationship } from '../api/relationships'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import type { RelationshipType } from '../api/types'

const relationshipTypes: RelationshipType[] = [
  'parent_of',
  'child_of',
  'blocks',
  'blocked_by',
  'related_to',
  'duplicate_of',
  'supersedes',
  'superseded_by',
]

/** Relationships are ticket-only, directed, typed edges (product spec
 * §5.7) — no version column, add/delete only. A duplicate `blocks`/
 * `parent_of` edge that would close a cycle is rejected server-side
 * with `relationship_cycle` (ADR 0014); that error surfaces through
 * `error` below like any other. */
export function RelationshipsSection({
  ticketRef,
  relationships,
  onChange,
  canEdit,
}: {
  ticketRef: string
  relationships: { type: RelationshipType; other: string }[]
  onChange: (relationships: { type: RelationshipType; other: string }[]) => void
  canEdit: boolean
}) {
  const [target, setTarget] = useState('')
  const [type, setType] = useState<RelationshipType>('related_to')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submitAdd() {
    setBusy(true)
    setError(null)
    try {
      await addRelationship(ticketRef, target, type)
      onChange([...relationships, { type, other: target }])
      setTarget('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleRemove(rel: { type: RelationshipType; other: string }) {
    setError(null)
    try {
      await removeRelationship(ticketRef, rel.type, rel.other)
      onChange(relationships.filter((r) => !(r.type === rel.type && r.other === rel.other)))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <>
      {relationships.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {relationships.map((r) => (
            <li key={`${r.type}-${r.other}`}>
              {r.type} <Link to={detailRoute(r.other)}>{r.other}</Link>
              {canEdit && (
                <button type="button" onClick={() => void handleRemove(r)}>
                  Remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {error && <p role="alert">{error}</p>}
      {canEdit && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submitAdd()
          }}
        >
          <label>
            Relationship
            <select value={type} onChange={(e) => setType(e.target.value as RelationshipType)}>
              {relationshipTypes.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label>
            Other ticket
            <input
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="ABC-2"
              required
            />
          </label>
          <button type="submit" disabled={busy}>
            Add relationship
          </button>
        </form>
      )}
    </>
  )
}
