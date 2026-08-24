import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { CONTENT_ITEM_LABELS, createContentItem, listContentItems } from '../api/content-items'
import type { ContentItemUrlKind } from '../api/content-items'
import { ApiError } from '../api/client'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { useAuth } from '../auth/AuthContext'
import type { ContentItemCompact } from '../api/types'

function NewContentItemForm({
  urlKind,
  singular,
  projectKey,
  onCreated,
}: {
  urlKind: ContentItemUrlKind
  singular: string
  projectKey: string
  onCreated: (item: ContentItemCompact) => void
}) {
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createContentItem(urlKind, projectKey, { title, body })
      onCreated({
        ref: created.ref,
        title: created.title,
        kind: created.kind,
        version: created.version,
        updated_at: created.updated_at,
      })
      setTitle('')
      setBody('')
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
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <MarkdownEditor label="Body" value={body} onChange={setBody} />
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : `Create ${singular}`}
      </button>
    </form>
  )
}

/** /projects/:key/plans and /projects/:key/documents both render this
 * — kind (from the route) picks the URL segment and copy; the two
 * views are otherwise identical, mirroring how ContentLibrary's server
 * side (internal/httpapi/content_items.go) shares one set of handlers
 * across both URL prefixes. */
export default function ContentLibrary({ kind }: { kind: ContentItemUrlKind }) {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const { singular, plural } = CONTENT_ITEM_LABELS[kind]
  const [items, setItems] = useState<ContentItemCompact[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    setItems(null)
    setError(null)
    setCreating(false)
    listContentItems(kind, key)
      .then((page) => setItems(page.items))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [kind, key])

  if (error) return <p role="alert">{error}</p>
  if (!items) return <p>Loading {kind}…</p>

  return (
    <main>
      <h1>
        {plural} — {key}
      </h1>
      {items.length === 0 ? (
        <p>No {kind} yet.</p>
      ) : (
        <ul>
          {items.map((item) => (
            <li key={item.ref}>
              <Link to={`/${kind}/${item.ref}`}>{item.title}</Link> <span>({item.ref})</span>
            </li>
          ))}
        </ul>
      )}

      {me?.permission === 'editor' &&
        (creating ? (
          <NewContentItemForm
            urlKind={kind}
            singular={singular}
            projectKey={key}
            onCreated={(item) => {
              setItems([...items, item])
              setCreating(false)
            }}
          />
        ) : (
          <button onClick={() => setCreating(true)}>New {singular}</button>
        ))}
    </main>
  )
}
