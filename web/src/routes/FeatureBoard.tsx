import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { listFeatures, reorderFeature, updateFeatureStatus } from '../api/features'
import { ApiError } from '../api/client'
import { useProjectChanged } from '../api/events'
import { useAuth } from '../auth/AuthContext'
import { StatusChip } from '../components/StatusChip'
import { useBoardDrag } from '../hooks/useBoardDrag'
import type { FeatureCompact, WorkflowStatus } from '../api/types'

const statuses: WorkflowStatus[] = [
  'backlog',
  'ready',
  'in_progress',
  'blocked',
  'review',
  'done',
  'cancelled',
]

const COLUMN_PAGE_SIZE = 50

interface ColumnState {
  features: FeatureCompact[] | null
  nextCursor?: string
  error: string | null
}

/** Feature board — same shape as TicketBoard. The status <select> on
 * every card moves it between columns; only possible since the Phase
 * 4 gap fix (POST /features/{ref}/status), before which a feature's
 * status could never change after creation. */
export default function FeatureBoard() {
  const { key = '' } = useParams()
  const { me } = useAuth()
  const [columns, setColumns] = useState<Record<WorkflowStatus, ColumnState>>(
    () =>
      Object.fromEntries(statuses.map((s) => [s, { features: null, error: null }])) as Record<
        WorkflowStatus,
        ColumnState
      >,
  )
  const canEdit = me?.permission === 'editor'
  // See TicketBoard's identical field: a card moving column remounts
  // its <select> in a different parent list, dropping focus with
  // nothing else to tell a screen-reader user what happened.
  const [moveAnnouncement, setMoveAnnouncement] = useState('')

  function currentFeature(ref: string): FeatureCompact | undefined {
    for (const status of statuses) {
      const found = columns[status].features?.find((f) => f.ref === ref)
      if (found) return found
    }
    return undefined
  }

  const reload = useCallback(
    (clear: boolean) => {
      if (clear) {
        setColumns(
          Object.fromEntries(statuses.map((s) => [s, { features: null, error: null }])) as Record<
            WorkflowStatus,
            ColumnState
          >,
        )
      }
      for (const status of statuses) {
        listFeatures(key, { status }, undefined, COLUMN_PAGE_SIZE)
          .then((page) => {
            setColumns((prev) => ({
              ...prev,
              [status]: { features: page.features, nextCursor: page.next_cursor, error: null },
            }))
          })
          .catch((err: unknown) => {
            setColumns((prev) => ({
              ...prev,
              [status]: { features: [], error: err instanceof ApiError ? err.message : String(err) },
            }))
          })
      }
    },
    [key],
  )

  useEffect(() => {
    reload(true)
  }, [reload])

  useProjectChanged(key, useCallback(() => reload(false), [reload]))

  async function loadMore(status: WorkflowStatus) {
    const col = columns[status]
    if (!col.nextCursor) return
    try {
      const page = await listFeatures(key, { status }, col.nextCursor, COLUMN_PAGE_SIZE)
      setColumns((prev) => ({
        ...prev,
        [status]: {
          features: [...(prev[status].features ?? []), ...page.features],
          nextCursor: page.next_cursor,
          error: null,
        },
      }))
    } catch (err) {
      setColumns((prev) => ({
        ...prev,
        [status]: { ...prev[status], error: err instanceof ApiError ? err.message : String(err) },
      }))
    }
  }

  async function applyReorder(dragged: FeatureCompact, afterRef: string | null) {
    const feature = currentFeature(dragged.ref) ?? dragged
    try {
      await reorderFeature(feature.ref, afterRef, feature.version)
      const page = await listFeatures(key, { status: feature.status }, undefined, COLUMN_PAGE_SIZE)
      setColumns((prev) => ({
        ...prev,
        [feature.status]: { features: page.features, nextCursor: page.next_cursor, error: null },
      }))
      setMoveAnnouncement(`Moved ${feature.ref}`)
    } catch (err) {
      setColumns((prev) => ({
        ...prev,
        [feature.status]: {
          ...prev[feature.status],
          error: err instanceof ApiError ? err.message : String(err),
        },
      }))
    }
  }

  async function reorder(status: WorkflowStatus, index: number, direction: -1 | 1) {
    const col = columns[status]
    const features = col.features
    if (!features) return
    const neighborIndex = index + direction
    if (neighborIndex < 0 || neighborIndex >= features.length) return
    const feature = features[index]
    const neighbor = features[neighborIndex]
    if (feature.priority !== neighbor.priority) return

    const afterRef =
      direction === 1
        ? neighbor.ref
        : (features[neighborIndex - 1]?.priority === feature.priority
            ? features[neighborIndex - 1].ref
            : null)
    await applyReorder(feature, afterRef)
  }

  async function moveCard(dragged: FeatureCompact, newStatus: WorkflowStatus) {
    const feature = currentFeature(dragged.ref) ?? dragged
    if (newStatus === feature.status) return
    try {
      const updated = await updateFeatureStatus(feature.ref, newStatus, feature.version)
      setColumns((prev) => {
        const next = { ...prev }
        next[feature.status] = {
          ...prev[feature.status],
          features: (prev[feature.status].features ?? []).filter((f) => f.ref !== feature.ref),
        }
        const moved: FeatureCompact = {
          ref: updated.ref,
          title: updated.title,
          status: updated.status,
          priority: updated.priority,
          version: updated.version,
          updated_at: updated.updated_at,
        }
        next[newStatus] = {
          ...prev[newStatus],
          features: [moved, ...(prev[newStatus].features ?? [])],
        }
        return next
      })
      setMoveAnnouncement(`Moved ${feature.ref} to ${newStatus}`)
    } catch (err) {
      setColumns((prev) => ({
        ...prev,
        [feature.status]: {
          ...prev[feature.status],
          error: err instanceof ApiError ? err.message : String(err),
        },
      }))
    }
  }

  const { dragging, dragProps, columnDropProps, cardDropProps } = useBoardDrag<FeatureCompact>(
    (f, afterRef) => void applyReorder(f, afterRef),
    (f, status) => void moveCard(f, status),
  )

  return (
    <main>
      <h1>Feature board — {key}</h1>
      <p aria-live="polite" className="sr-only">
        {moveAnnouncement}
      </p>
      <div className="board">
        {statuses.map((status) => {
          const col = columns[status]
          const features = col.features
          return (
            <section key={status} className="board-column">
              <h2>
                <StatusChip value={status} kind="status" />
              </h2>
              {col.error && <p role="alert">{col.error}</p>}
              {!features ? (
                <p>Loading…</p>
              ) : (
                <div className="board-column-list" {...(canEdit ? columnDropProps(status) : {})}>
                  <ul>
                    {features.map((f, i) => (
                      <li
                        key={f.ref}
                        className={
                          dragging?.ref === f.ref ? 'board-card board-card-dragging' : 'board-card'
                        }
                        {...(canEdit ? dragProps(f) : {})}
                        {...(canEdit ? cardDropProps(f, features) : {})}
                      >
                        <Link to={`/features/${f.ref}`}>{f.ref}</Link>
                        <p>{f.title}</p>
                        <p>
                          <StatusChip value={f.priority} kind="priority" />
                        </p>
                        {canEdit && (
                          <div className="reorder-cell">
                            <button
                              type="button"
                              onClick={() => void reorder(status, i, -1)}
                              disabled={i === 0 || features[i - 1].priority !== f.priority}
                              aria-label={`Move ${f.ref} up`}
                            >
                              ↑
                            </button>
                            <button
                              type="button"
                              onClick={() => void reorder(status, i, 1)}
                              disabled={
                                i === features.length - 1 || features[i + 1].priority !== f.priority
                              }
                              aria-label={`Move ${f.ref} down`}
                            >
                              ↓
                            </button>
                          </div>
                        )}
                        {canEdit && (
                          <label>
                            Move to
                            <select
                              value={status}
                              onChange={(e) => void moveCard(f, e.target.value as WorkflowStatus)}
                            >
                              {statuses.map((s) => (
                                <option key={s} value={s}>
                                  {s}
                                </option>
                              ))}
                            </select>
                          </label>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {col.nextCursor && <button onClick={() => void loadMore(status)}>Load more</button>}
            </section>
          )
        })}
      </div>
    </main>
  )
}
