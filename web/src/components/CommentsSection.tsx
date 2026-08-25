import { useState } from 'react'
import { addComment, deleteComment, editComment, getComment } from '../api/comments'
import { ApiError } from '../api/client'
import { Markdown } from './Markdown'
import type { CommentDetail } from '../api/types'

/** A comment's version is independent of its parent ticket's
 * (docs/contracts/concurrency.md) — a stale comment edit never
 * implies the ticket itself is stale, so this gets its own simpler
 * conflict UX (plan.md §3 step 5: base-vs-theirs, not a per-field
 * merge like useConflictForm) rather than reusing the ticket/feature
 * three-way-merge machinery. */
function EditingComment({
  comment,
  onSaved,
  onCancel,
}: {
  comment: CommentDetail
  onSaved: (updated: CommentDetail) => void
  onCancel: () => void
}) {
  const [body, setBody] = useState(comment.body)
  const [expectedVersion, setExpectedVersion] = useState(comment.version)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [theirs, setTheirs] = useState<CommentDetail | null>(null)

  async function save() {
    setSaving(true)
    setError(null)
    try {
      const updated = await editComment(comment.id, body, expectedVersion)
      onSaved(updated)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        const live = await getComment(comment.id)
        setTheirs(live)
        setExpectedVersion(live.version)
      } else {
        setError(err instanceof ApiError ? err.message : String(err))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <li>
      <textarea
        aria-label="Edit comment"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={4}
      />
      {theirs && (
        <div role="alert">
          <p>
            This comment was edited by someone else while you were editing (now version{' '}
            {theirs.version}):
          </p>
          <blockquote>
            <Markdown>{theirs.body}</Markdown>
          </blockquote>
          <p>Saving now will overwrite their edit with your text above.</p>
        </div>
      )}
      {error && <p role="alert">{error}</p>}
      <button type="button" onClick={() => void save()} disabled={saving}>
        {saving ? 'Saving…' : theirs ? 'Overwrite and save' : 'Save'}
      </button>
      <button type="button" onClick={onCancel}>
        Cancel
      </button>
    </li>
  )
}

export function CommentsSection({
  entityRef,
  comments,
  onChange,
  canEdit,
}: {
  /** Any of the six commentable references §5.10 names: a
   * ticket/feature/decision/plan/document reference, or a bare
   * project key (Phase 6 Step 2). */
  entityRef: string
  comments: CommentDetail[]
  /** Called with the updated comment array only — a comment mutation
   * never bumps the parent entity's version, so the caller updates
   * just its own `comments` field, no parent refetch needed. */
  onChange: (comments: CommentDetail[]) => void
  canEdit: boolean
}) {
  const [newBody, setNewBody] = useState('')
  const [posting, setPosting] = useState(false)
  const [postError, setPostError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  async function submitNew() {
    if (!newBody.trim()) return
    setPosting(true)
    setPostError(null)
    try {
      const created = await addComment(entityRef, newBody)
      onChange([...comments, created])
      setNewBody('')
    } catch (err) {
      setPostError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setPosting(false)
    }
  }

  async function handleDelete(comment: CommentDetail) {
    setDeleteError(null)
    try {
      await deleteComment(comment.id, comment.version)
      onChange(comments.filter((c) => c.id !== comment.id))
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        const live = await getComment(comment.id)
        onChange(comments.map((c) => (c.id === comment.id ? live : c)))
        setDeleteError(
          'That comment changed since this page loaded — refreshed it below; try deleting again if you still want to.',
        )
      } else {
        setDeleteError(err instanceof ApiError ? err.message : String(err))
      }
    }
  }

  return (
    <>
      {comments.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {comments.map((c) =>
            editingId === c.id ? (
              <EditingComment
                key={c.id}
                comment={c}
                onSaved={(updated) => {
                  onChange(comments.map((existing) => (existing.id === c.id ? updated : existing)))
                  setEditingId(null)
                }}
                onCancel={() => setEditingId(null)}
              />
            ) : (
              <li key={c.id}>
                <p>
                  <strong>{c.author}</strong> — {c.created_at}
                  {c.deleted_at ? ' (deleted)' : ''}
                </p>
                <Markdown>{c.body}</Markdown>
                {canEdit && !c.deleted_at && (
                  <>
                    <button type="button" onClick={() => setEditingId(c.id)}>
                      Edit
                    </button>
                    <button type="button" onClick={() => void handleDelete(c)}>
                      Delete
                    </button>
                  </>
                )}
              </li>
            ),
          )}
        </ul>
      )}
      {deleteError && <p role="alert">{deleteError}</p>}

      {canEdit && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submitNew()
          }}
        >
          <textarea
            aria-label="Add a comment"
            value={newBody}
            onChange={(e) => setNewBody(e.target.value)}
            rows={3}
            placeholder="Add a comment…"
          />
          {postError && <p role="alert">{postError}</p>}
          <button type="submit" disabled={posting || !newBody.trim()}>
            {posting ? 'Posting…' : 'Add comment'}
          </button>
        </form>
      )}
    </>
  )
}
