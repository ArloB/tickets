import { useEffect } from 'react'

/** The wire shape of one SSE "data:" line (internal/httpapi/events.go's
 * changeHintWire) — an invalidation signal only (product spec §17: "treat
 * SSE as invalidation/change hints only; refetch authoritative state from
 * the API"). Never carries a changed value, only enough to know what to
 * refetch. */
export interface ChangeHint {
  ref?: string
  project?: string
  actor?: string
}

type HintKind = 'entity_changed' | 'notifications_changed'
type Listener = (hint: ChangeHint) => void

const listeners: Record<HintKind, Set<Listener>> = {
  entity_changed: new Set(),
  notifications_changed: new Set(),
}

let source: EventSource | null = null

/** Opens the one shared GET /api/v1/events connection for the app's
 * lifetime (idempotent — a second call is a no-op). Same-origin, so
 * the session cookie rides along automatically; no withCredentials
 * needed (that only matters cross-origin). Called once from
 * Layout.tsx, which mounts for every authenticated route. */
export function connectChangeHints(): void {
  if (source) return
  source = new EventSource('/api/v1/events')
  for (const kind of Object.keys(listeners) as HintKind[]) {
    source.addEventListener(kind, (event) => {
      let hint: ChangeHint = {}
      try {
        hint = JSON.parse((event as MessageEvent<string>).data) as ChangeHint
      } catch {
        return // a malformed hint is never authoritative anyway — drop it
      }
      for (const listener of listeners[kind]) listener(hint)
    })
  }
}

/** Subscribes to one hint kind; returns an unsubscribe function.
 * Exported mainly for the two hooks below — a component wanting more
 * control than "does this ref/project match" can still use it
 * directly. */
export function onChangeHint(kind: HintKind, listener: Listener): () => void {
  listeners[kind].add(listener)
  return () => {
    listeners[kind].delete(listener)
  }
}

/** Calls onMatch whenever an entity_changed hint's ref equals ref —
 * a detail page's "someone else changed this record, refetch it"
 * signal. No-op while ref is falsy (nothing loaded yet to match
 * against). */
export function useEntityChanged(ref: string | undefined, onMatch: () => void): void {
  useEffect(() => {
    if (!ref) return
    return onChangeHint('entity_changed', (hint) => {
      if (hint.ref === ref) onMatch()
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref, onMatch])
}

/** Calls onMatch whenever an entity_changed hint's project equals
 * projectKey — a board/list page's "something in this project
 * changed, refetch the list" signal. */
export function useProjectChanged(projectKey: string | undefined, onMatch: () => void): void {
  useEffect(() => {
    if (!projectKey) return
    return onChangeHint('entity_changed', (hint) => {
      if (hint.project === projectKey) onMatch()
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectKey, onMatch])
}

/** Calls onMatch on every notifications_changed hint. No ref/project
 * filtering needed: internal/httpapi's Hub only ever delivers this
 * kind to the one connection whose authenticated actor matches
 * (ADR 0020) — every hint this listener sees is already this user's
 * own. */
export function useNotificationsChanged(onMatch: () => void): void {
  useEffect(() => {
    return onChangeHint('notifications_changed', () => onMatch())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onMatch])
}
