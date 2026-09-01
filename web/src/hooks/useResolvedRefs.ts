import { useEffect, useState } from 'react'
import { resolveRefs, type ResolvedRef } from '../api/refs'
import { scanRefs } from '../components/refLinks'

const resolutionCache = new Map<string, ResolvedRef>()

export function useResolvedRefs(text: string, projectKey: string): Map<string, ResolvedRef> {
  const [resolved, setResolved] = useState<Map<string, ResolvedRef>>(resolutionCache)

  useEffect(() => {
    const tokens = [...new Set(scanRefs(text, projectKey).map((m) => m.token))]
    const missing = tokens.filter((t) => !resolutionCache.has(t))
    if (missing.length === 0) {
      setResolved(new Map(resolutionCache))
      return
    }

    let live = true
    resolveRefs(missing)
      .then((results) => {
        for (const r of results) {
          if (r.exists) resolutionCache.set(r.ref, r)
        }
        if (live) setResolved(new Map(resolutionCache))
      })
      .catch(() => {})
    return () => {
      live = false
    }
  }, [text, projectKey])

  return resolved
}
