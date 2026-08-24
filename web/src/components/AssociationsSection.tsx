import { useState } from 'react'
import { Link } from 'react-router-dom'
import { addAssociation, removeAssociation } from '../api/associations'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'

/** Associations span tickets, features, and decisions — one loose,
 * non-directional `associated_with` edge (product spec §5.7), unlike
 * relationships (ticket-only, directed, typed). Add/remove return no
 * body (docs/contracts/concurrency.md: no version column either), so
 * this maintains the list optimistically from the ref the caller
 * already typed rather than refetching. */
export function AssociationsSection({
  entityRef,
  associated,
  onChange,
  canEdit,
}: {
  entityRef: string
  associated: string[]
  onChange: (refs: string[]) => void
  canEdit: boolean
}) {
  const [target, setTarget] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submitAdd() {
    setBusy(true)
    setError(null)
    try {
      await addAssociation(entityRef, target)
      onChange([...associated, target])
      setTarget('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleRemove(ref: string) {
    setError(null)
    try {
      await removeAssociation(entityRef, ref)
      onChange(associated.filter((r) => r !== ref))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <>
      {associated.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {associated.map((ref) => (
            <li key={ref}>
              <Link to={detailRoute(ref)}>{ref}</Link>
              {canEdit && (
                <button type="button" onClick={() => void handleRemove(ref)}>
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
            Associate with
            <input
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="ABC-1, ABC-F1, ABC-D1…"
              required
            />
          </label>
          <button type="submit" disabled={busy}>
            Associate
          </button>
        </form>
      )}
    </>
  )
}
