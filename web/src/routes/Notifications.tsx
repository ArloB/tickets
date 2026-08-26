import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listNotifications, markAllNotificationsRead, markNotificationsRead } from '../api/notifications'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { useNotificationsChanged } from '../api/events'
import type { Notification } from '../api/types'

/** The notification inbox (§6.4/§6.5). */
export default function Notifications() {
  const [notifications, setNotifications] = useState<Notification[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)

  const reload = useCallback(() => {
    setNotifications(null)
    setNextCursor(undefined)
    setError(null)
    listNotifications({ unread: unreadOnly })
      .then((page) => {
        setNotifications(page.notifications)
        setNextCursor(page.next_cursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [unreadOnly])

  useEffect(() => {
    reload()
  }, [reload])

  // A new notification arriving while this page is already open — the
  // hint is scoped server-side to this actor already (ADR 0020), so
  // any receipt here means "reload the inbox," full stop.
  useNotificationsChanged(reload)

  async function loadMore() {
    if (!notifications || !nextCursor) return
    setLoadingMore(true)
    try {
      const page = await listNotifications({ unread: unreadOnly, cursor: nextCursor })
      setNotifications([...notifications, ...page.notifications])
      setNextCursor(page.next_cursor)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoadingMore(false)
    }
  }

  async function markOneRead(id: number) {
    try {
      await markNotificationsRead([id])
      setNotifications((prev) => prev?.map((n) => (n.id === id ? { ...n, read_at: new Date().toISOString() } : n)) ?? null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  async function markAllRead() {
    try {
      await markAllNotificationsRead()
      setNotifications((prev) => prev?.map((n) => ({ ...n, read_at: n.read_at ?? new Date().toISOString() })) ?? null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  if (error) return <p role="alert">{error}</p>

  return (
    <main>
      <h1>Notifications</h1>
      <div className="inline-controls">
        <label>
          <input type="checkbox" checked={unreadOnly} onChange={(e) => setUnreadOnly(e.target.checked)} />
          Unread only
        </label>
        <button onClick={() => void markAllRead()}>Mark all read</button>
      </div>

      {!notifications ? (
        <p>Loading…</p>
      ) : notifications.length === 0 ? (
        <p>No notifications.</p>
      ) : (
        <ul>
          {notifications.map((n) => (
            <li key={n.id}>
              <time dateTime={n.created_at}>{new Date(n.created_at).toLocaleString()}</time>{' '}
              <span>{n.kind}</span>{' '}
              <Link to={detailRoute(n.entity)}>{n.entity}</Link>
              {n.triggered_by && <span> from {n.triggered_by}</span>}
              {n.read_at ? <span> (read)</span> : <button onClick={() => void markOneRead(n.id)}>Mark read</button>}
            </li>
          ))}
        </ul>
      )}
      {nextCursor && (
        <button onClick={() => void loadMore()} disabled={loadingMore}>
          {loadingMore ? 'Loading…' : 'Load more'}
        </button>
      )}
    </main>
  )
}
