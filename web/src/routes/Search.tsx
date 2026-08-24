import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { search } from '../api/search'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import type { SearchHit } from '../api/types'

/** GET /search's web view (§5.12). Global by default — q comes from
 * the URL (?q=), so a search from Layout's nav box, a bookmark, or a
 * shared link all land here the same way. */
export default function Search() {
  const [params] = useSearchParams()
  const query = params.get('q') ?? ''
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)

  useEffect(() => {
    setHits(null)
    setNextCursor(undefined)
    setError(null)
    if (!query) {
      setHits([])
      return
    }
    search(query)
      .then((page) => {
        setHits(page.hits)
        setNextCursor(page.next_cursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [query])

  async function loadMore() {
    if (!hits || !nextCursor) return
    setLoadingMore(true)
    try {
      const page = await search(query, { cursor: nextCursor })
      setHits([...hits, ...page.hits])
      setNextCursor(page.next_cursor)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoadingMore(false)
    }
  }

  return (
    <main>
      <h1>Search{query ? ` — ${query}` : ''}</h1>
      {error && <p role="alert">{error}</p>}
      {!error && !query && <p>Enter a search term above.</p>}
      {!error && query && !hits && <p>Searching…</p>}
      {!error && hits && hits.length === 0 && <p>No results for &quot;{query}&quot;.</p>}
      {!error && hits && hits.length > 0 && (
        <ul>
          {hits.map((h) => (
            <li key={`${h.kind}-${h.ref}-${h.comment_id ?? 'own'}`}>
              <Link to={detailRoute(h.ref)}>{h.ref}</Link> <span>({h.kind})</span>
              {h.title && <strong> {h.title}</strong>}
              <p>{h.snippet}</p>
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
