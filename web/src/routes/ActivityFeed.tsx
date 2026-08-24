import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { listActivity } from '../api/activity'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import type { ActivityEvent } from '../api/types'

export default function ActivityFeed() {
  const { key = '' } = useParams()
  const [events, setEvents] = useState<ActivityEvent[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)

  useEffect(() => {
    setEvents(null)
    setNextCursor(undefined)
    setError(null)
    listActivity(key)
      .then((page) => {
        setEvents(page.events)
        setNextCursor(page.next_cursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [key])

  async function loadMore() {
    if (!events || !nextCursor) return
    setLoadingMore(true)
    try {
      const page = await listActivity(key, { cursor: nextCursor })
      setEvents([...events, ...page.events])
      setNextCursor(page.next_cursor)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoadingMore(false)
    }
  }

  if (error) return <p role="alert">{error}</p>
  if (!events) return <p>Loading activity…</p>

  return (
    <main>
      <h1>Activity — {key}</h1>
      {events.length === 0 ? (
        <p>No activity yet.</p>
      ) : (
        <ul>
          {events.map((e) => (
            <li key={e.id}>
              <time dateTime={e.created_at}>{new Date(e.created_at).toLocaleString()}</time>{' '}
              <span>{e.actor}</span> <span>{e.event_type}</span>{' '}
              {e.entity ? <Link to={detailRoute(e.entity)}>{e.entity}</Link> : <span>({key})</span>}
              {e.comment_excerpt && <p>{e.comment_excerpt}</p>}
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
