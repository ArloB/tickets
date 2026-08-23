import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { listFeatures, updateFeatureStatus } from '../api/features'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
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

/** Feature board — same shape as TicketBoard, moving cards via a
 * status <select> (keyboard-operable, no pointer drag). Only possible
 * since the Phase 4 gap fix (POST /features/{ref}/status): before
 * that, a feature's status could never change after creation. */
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

  useEffect(() => {
    setColumns(
      Object.fromEntries(statuses.map((s) => [s, { features: null, error: null }])) as Record<
        WorkflowStatus,
        ColumnState
      >,
    )
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
  }, [key])

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

  async function moveCard(feature: FeatureCompact, newStatus: WorkflowStatus) {
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

  return (
    <main>
      <h1>Feature board — {key}</h1>
      <div className="board">
        {statuses.map((status) => {
          const col = columns[status]
          return (
            <section key={status} className="board-column">
              <h2>{status}</h2>
              {col.error && <p role="alert">{col.error}</p>}
              {!col.features ? (
                <p>Loading…</p>
              ) : (
                <ul>
                  {col.features.map((f) => (
                    <li key={f.ref} className="board-card">
                      <Link to={`/features/${f.ref}`}>{f.ref}</Link>
                      <p>{f.title}</p>
                      <p>{f.priority}</p>
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
              )}
              {col.nextCursor && <button onClick={() => void loadMore(status)}>Load more</button>}
            </section>
          )
        })}
      </div>
    </main>
  )
}
