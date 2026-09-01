import { Link, useParams } from 'react-router-dom'
import { listActivity } from '../api/activity'
import { detailRoute } from '../api/refs'
import { InlineRefText } from '../components/InlineRefText'
import { Pager } from '../components/Pager'
import { useCursorPager } from '../hooks/useCursorPager'
import type { ActivityEvent } from '../api/types'

export default function ActivityFeed() {
  const { key = '' } = useParams()

  const {
    items: events,
    error,
    loading,
    hasNext,
    hasPrev,
    next,
    prev,
  } = useCursorPager<ActivityEvent>(
    (cursor) => listActivity(key, { cursor }).then((page) => ({ items: page.events, nextCursor: page.next_cursor })),
    [key],
  )

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
            <li key={e.id} className={e.comment_excerpt ? undefined : 'activity-mechanical'}>
              <time dateTime={e.created_at}>{new Date(e.created_at).toLocaleString()}</time>{' '}
              <span>{e.actor}</span> <span>{e.event_type}</span>{' '}
              {e.entity ? <Link to={detailRoute(e.entity)}>{e.entity}</Link> : <span>({key})</span>}
              {e.comment_excerpt && (
                <p>
                  <InlineRefText text={e.comment_excerpt} projectKey={key} />
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
      <Pager hasPrev={hasPrev} hasNext={hasNext} loading={loading} onPrev={prev} onNext={next} />
    </main>
  )
}
