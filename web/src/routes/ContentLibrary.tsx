import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { CONTENT_ITEM_LABELS, createContentItem, listContentItems, uploadContentItem } from '../api/content-items'
import type { ContentItemUrlKind } from '../api/content-items'
import { ApiError } from '../api/client'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { Pager } from '../components/Pager'
import { useAuth } from '../auth/AuthContext'
import { useCursorPager } from '../hooks/useCursorPager'
import type { ContentItemCompact, ContentItemRepresentation } from '../api/types'

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
  const [representation, setRepresentation] = useState<ContentItemRepresentation>('markdown')
  const [body, setBody] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [pathValue, setPathValue] = useState('')
  const [urlValue, setUrlValue] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function reset() {
    setTitle('')
    setBody('')
    setFile(null)
    setPathValue('')
    setUrlValue('')
  }

  async function submit() {
    if (representation === 'file' && !file) {
      setError('Choose a file to upload.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const created =
        representation === 'file'
          ? await uploadContentItem(urlKind, projectKey, title, file!)
          : await createContentItem(urlKind, projectKey, {
              title,
              representation,
              body: representation === 'markdown' ? body : undefined,
              path: representation === 'path' ? pathValue : undefined,
              url: representation === 'url' ? urlValue : undefined,
            })
      onCreated({
        ref: created.ref,
        title: created.title,
        kind: created.kind,
        version: created.version,
        updated_at: created.updated_at,
      })
      reset()
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
      <fieldset>
        <legend>Representation</legend>
        {(['markdown', 'file', 'path', 'url'] as const).map((r) => (
          <label key={r}>
            <input
              type="radio"
              name="content-item-representation"
              checked={representation === r}
              onChange={() => setRepresentation(r)}
            />
            {r}
          </label>
        ))}
      </fieldset>
      {representation === 'markdown' && <MarkdownEditor label="Body" value={body} onChange={setBody} projectKey={projectKey} />}
      {representation === 'file' && (
        <label>
          File
          <input type="file" onChange={(e) => setFile(e.target.files?.[0] ?? null)} required />
        </label>
      )}
      {representation === 'path' && (
        <label>
          Path
          <input
            value={pathValue}
            onChange={(e) => setPathValue(e.target.value)}
            placeholder="/path/to/file"
            required
          />
        </label>
      )}
      {representation === 'url' && (
        <label>
          URL
          <input
            value={urlValue}
            onChange={(e) => setUrlValue(e.target.value)}
            placeholder="https://…"
            required
          />
        </label>
      )}
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
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    setCreating(false)
  }, [kind, key])

  const {
    items,
    setItems,
    error,
    loading,
    hasNext,
    hasPrev,
    next,
    prev,
  } = useCursorPager<ContentItemCompact>(
    (cursor) => listContentItems(kind, key, cursor).then((page) => ({ items: page.items, nextCursor: page.next_cursor })),
    [kind, key],
  )

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
      <Pager hasPrev={hasPrev} hasNext={hasNext} loading={loading} onPrev={prev} onNext={next} />

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
