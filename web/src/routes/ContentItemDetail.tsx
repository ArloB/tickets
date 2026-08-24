import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  CONTENT_ITEM_LABELS,
  getContentItem,
  getContentItemDiff,
  listContentItemVersions,
  updateContentItem,
} from '../api/content-items'
import type { ContentItemUrlKind } from '../api/content-items'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listBacklinks } from '../api/backlinks'
import { listAttachments } from '../api/attachments'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { Markdown } from '../components/Markdown'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { AssociationsSection } from '../components/AssociationsSection'
import { LinksSection } from '../components/LinksSection'
import { AttachmentList } from '../components/AttachmentList'
import { DiffView } from '../components/DiffView'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  ContentItemDetail as ContentItemDetailDto,
  ContentItemDiff,
  ContentItemVersion,
  ExternalLink,
} from '../api/types'

function VersionHistory({ urlKind, item }: { urlKind: ContentItemUrlKind; item: ContentItemDetailDto }) {
  const [versions, setVersions] = useState<ContentItemVersion[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [from, setFrom] = useState<number | null>(null)
  const [to, setTo] = useState<number>(item.version)
  const [diff, setDiff] = useState<ContentItemDiff | null>(null)
  const [diffError, setDiffError] = useState<string | null>(null)

  useEffect(() => {
    setVersions(null)
    setDiff(null)
    listContentItemVersions(urlKind, item.ref)
      .then((page) => {
        setVersions(page.versions)
        setFrom(page.versions.length > 0 ? page.versions[0].version : null)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [urlKind, item.ref])

  async function showDiff() {
    if (from === null) return
    setDiffError(null)
    try {
      setDiff(await getContentItemDiff(urlKind, item.ref, from, to))
    } catch (err) {
      setDiffError(err instanceof ApiError ? err.message : String(err))
    }
  }

  if (error) return <p role="alert">{error}</p>
  if (!versions) return <p>Loading version history…</p>
  if (versions.length === 0) return <p>No prior versions — this is the only version.</p>

  const allVersions = [...versions.map((v) => v.version), item.version]

  return (
    <div>
      <table>
        <thead>
          <tr>
            <th>Version</th>
            <th>Title</th>
            <th>Edited by</th>
            <th>Edited at</th>
          </tr>
        </thead>
        <tbody>
          {versions.map((v) => (
            <tr key={v.version}>
              <td>{v.version}</td>
              <td>{v.title}</td>
              <td>{v.edited_by}</td>
              <td>{new Date(v.created_at).toLocaleString()}</td>
            </tr>
          ))}
          <tr>
            <td>{item.version} (current)</td>
            <td>{item.title}</td>
            <td colSpan={2}></td>
          </tr>
        </tbody>
      </table>

      <form
        onSubmit={(e) => {
          e.preventDefault()
          void showDiff()
        }}
      >
        <label>
          From
          <select value={from ?? ''} onChange={(e) => setFrom(Number(e.target.value))}>
            {allVersions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label>
          To
          <select value={to} onChange={(e) => setTo(Number(e.target.value))}>
            {allVersions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <button type="submit">Show diff</button>
      </form>

      {diffError && <p role="alert">{diffError}</p>}
      {diff && (
        <div data-testid="version-diff">
          {([
            ['Title', diff.title],
            ['Body', diff.body],
          ] as const).map(([label, lines]) => (
            <div key={label}>
              <h3>{label}</h3>
              <DiffView lines={lines} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** /plans/:ref and /documents/:ref both render this — urlKind (from
 * the route) picks the API path segment; everything else (fields,
 * version history, associations/links/backlinks) is identical, since
 * plans and documents share one server-side table/handler set. */
export default function ContentItemDetail({ urlKind }: { urlKind: ContentItemUrlKind }) {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [item, setItem] = useState<ContentItemDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [attachments, setAttachments] = useState<Attachment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [editTitle, setEditTitle] = useState('')
  const [editBody, setEditBody] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    setItem(null)
    setLinks(null)
    setAssociated(null)
    setBacklinks(null)
    setAttachments(null)
    setError(null)
    setEditing(false)
    Promise.all([
      getContentItem(urlKind, ref),
      listLinks(ref),
      listAssociations(ref),
      listBacklinks(ref),
      listAttachments(ref),
    ])
      .then(([d, l, a, b, at]) => {
        setItem(d)
        setLinks(l.links)
        setAssociated(a.associated)
        setBacklinks(b.backlinks)
        setAttachments(at.attachments)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [urlKind, ref])

  if (error) return <p role="alert">{error}</p>
  if (!item) return <p>Loading…</p>

  const canEdit = me?.permission === 'editor'
  const { singular } = CONTENT_ITEM_LABELS[urlKind]

  function startEditing() {
    if (!item) return
    setEditTitle(item.title)
    setEditBody(item.body)
    setSaveError(null)
    setEditing(true)
  }

  async function save() {
    if (!item) return
    setSaving(true)
    setSaveError(null)
    try {
      const updated = await updateContentItem(urlKind, item.ref, { title: editTitle, body: editBody }, item.version)
      setItem(updated)
      setEditing(false)
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <main>
        <h1>
          Edit {item.title} <span>({item.ref})</span>
        </h1>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label>
            Title
            <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} required />
          </label>
          <MarkdownEditor label="Body" value={editBody} onChange={setEditBody} />
          {saveError && <p role="alert">{saveError}</p>}
          <button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
          <button type="button" onClick={() => setEditing(false)} disabled={saving}>
            Cancel
          </button>
        </form>
      </main>
    )
  }

  return (
    <main>
      <h1>
        {item.title} <span>({item.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${item.project}`}>{item.project}</Link> · {singular}
      </p>
      {canEdit && <button onClick={startEditing}>Edit</button>}

      <h2>Body</h2>
      <Markdown>{item.body}</Markdown>

      <h2>Associations</h2>
      {associated && (
        <AssociationsSection entityRef={item.ref} associated={associated} onChange={setAssociated} canEdit={canEdit} />
      )}

      <h2>Links</h2>
      {links && <LinksSection entityRef={item.ref} links={links} onChange={setLinks} canEdit={canEdit} />}

      <h2>Attachments</h2>
      {attachments && (
        <AttachmentList
          ownerRef={item.ref}
          attachments={attachments}
          onChange={setAttachments}
          canEdit={canEdit}
        />
      )}

      <h2>Backlinks</h2>
      {!backlinks || backlinks.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {backlinks.map((b) => (
            <li key={`${b.ref}-${b.comment_id ?? 'body'}`}>
              <Link to={detailRoute(b.ref)}>{b.ref}</Link>
              {b.comment_id !== undefined ? ` (comment #${b.comment_id})` : ' (description)'}
            </li>
          ))}
        </ul>
      )}

      <h2>Version history</h2>
      <VersionHistory urlKind={urlKind} item={item} />
    </main>
  )
}
