import { useState } from 'react'
import {
  addPathAttachment,
  attachmentDownloadUrl,
  deleteAttachment,
  uploadAttachment,
} from '../api/attachments'
import { ApiError } from '../api/client'
import type { Attachment } from '../api/types'

export function AttachmentList({
  ownerRef,
  commentId,
  attachments,
  onChange,
  canEdit,
}: {
  ownerRef?: string
  commentId?: number
  attachments: Attachment[]
  onChange: (attachments: Attachment[]) => void
  canEdit: boolean
}) {
  const [title, setTitle] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [pathValue, setPathValue] = useState('')
  const [mode, setMode] = useState<'upload' | 'path'>('upload')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submitAdd() {
    if (mode === 'upload' && !file) {
      setError('Choose a file to upload.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const created =
        mode === 'upload'
          ? await uploadAttachment(ownerRef, commentId, title, file!)
          : await addPathAttachment(ownerRef, commentId, title, pathValue)
      onChange([...attachments, created])
      setTitle('')
      setFile(null)
      setPathValue('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleRemove(a: Attachment) {
    setError(null)
    try {
      await deleteAttachment(a.id, a.current_version)
      onChange(attachments.filter((x) => x.id !== a.id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <>
      {attachments.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {attachments.map((a) => (
            <li key={a.id}>
              {a.kind === 'upload' ? (
                <a href={attachmentDownloadUrl(a.id)}>{a.title}</a>
              ) : (
                <span>
                  {a.title} ({a.path_value})
                </span>
              )}
              {' — v'}
              {a.current_version}
              {canEdit && (
                <button type="button" onClick={() => void handleRemove(a)}>
                  Remove attachment
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
            Attachment title
            <input value={title} onChange={(e) => setTitle(e.target.value)} required />
          </label>
          <label>
            <input
              type="radio"
              name="attachment-mode"
              checked={mode === 'upload'}
              onChange={() => setMode('upload')}
            />
            Upload a file
          </label>
          <label>
            <input
              type="radio"
              name="attachment-mode"
              checked={mode === 'path'}
              onChange={() => setMode('path')}
            />
            Reference a path
          </label>
          {mode === 'upload' ? (
            <label>
              File
              <input
                type="file"
                onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                required
              />
            </label>
          ) : (
            <input
              value={pathValue}
              onChange={(e) => setPathValue(e.target.value)}
              placeholder="/path/to/file"
              required
            />
          )}
          <button type="submit" disabled={busy}>
            Add attachment
          </button>
        </form>
      )}
    </>
  )
}
