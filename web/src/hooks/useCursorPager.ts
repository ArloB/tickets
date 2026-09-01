import { useEffect, useState } from 'react'
import { ApiError } from '../api/client'

export interface CursorPage<T> {
  items: T[]
  nextCursor?: string
}

export interface CursorPager<T> {
  items: T[] | null
  setItems: (items: T[]) => void
  error: string | null
  loading: boolean
  hasNext: boolean
  hasPrev: boolean
  next: () => void
  prev: () => void
}

export function useCursorPager<T>(
  fetchPage: (cursor: string | undefined) => Promise<CursorPage<T>>,
  deps: unknown[],
): CursorPager<T> {
  const [items, setItems] = useState<T[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [history, setHistory] = useState<Array<string | undefined>>([])

  function load(c: string | undefined) {
    setLoading(true)
    setError(null)
    fetchPage(c)
      .then((page) => {
        setItems(page.items)
        setNextCursor(page.nextCursor)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
      .finally(() => setLoading(false))
  }

  /* eslint-disable-next-line react-hooks/exhaustive-deps */
  useEffect(() => {
    setHistory([])
    setCursor(undefined)
    setItems(null)
    setError(null)
    load(undefined)
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, deps)

  function next() {
    if (!nextCursor) return
    setHistory((h) => [...h, cursor])
    setCursor(nextCursor)
    load(nextCursor)
  }

  function prev() {
    if (history.length === 0) return
    const c = history[history.length - 1]
    setHistory((h) => h.slice(0, -1))
    setCursor(c)
    load(c)
  }

  return {
    items,
    setItems,
    error,
    loading,
    hasNext: nextCursor !== undefined,
    hasPrev: history.length > 0,
    next,
    prev,
  }
}
