import { useState } from 'react'
import { addLink, removeLink } from '../api/links'
import { ApiError } from '../api/client'
import type { ExternalLink } from '../api/types'

/** External links have no version column (docs/contracts/
 * concurrency.md's Phase 4 addendum) — add/delete only, no in-place
 * edit; change title/URL by removing and re-adding. Shared across
 * tickets, features, and decisions via api/links.ts's ref-based
 * routing. */
export function LinksSection({
  entityRef,
  links,
  onChange,
  canEdit,
}: {
  entityRef: string
  links: ExternalLink[]
  onChange: (links: ExternalLink[]) => void
  canEdit: boolean
}) {
  const [title, setTitle] = useState('')
  const [url, setUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submitAdd() {
    setBusy(true)
    setError(null)
    try {
      const created = await addLink(entityRef, title, url)
      onChange([...links, created])
      setTitle('')
      setUrl('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleRemove(id: number) {
    setError(null)
    try {
      await removeLink(entityRef, id)
      onChange(links.filter((l) => l.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <>
      {links.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {links.map((l) => (
            <li key={l.id}>
              <a href={l.url} target="_blank" rel="noreferrer">
                {l.title}
              </a>
              {canEdit && (
                <button type="button" onClick={() => void handleRemove(l.id)}>
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
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Title"
            required
          />
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://…"
            required
          />
          <button type="submit" disabled={busy}>
            Add link
          </button>
        </form>
      )}
    </>
  )
}
