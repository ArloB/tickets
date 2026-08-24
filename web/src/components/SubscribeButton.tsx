import { useEffect, useState } from 'react'
import { getSubscription, subscribe, unsubscribe } from '../api/subscriptions'
import { ApiError } from '../api/client'

/** Subscribe/unsubscribe toggle (§6.4) — self-contained: fetches its
 * own subscription state for ref and manages the toggle, so a detail
 * page only has to render <SubscribeButton ref={item.ref} /> without
 * threading subscription state through its own load effect. Renders
 * nothing (not even an error) for an anonymous viewer — there is no
 * subscription to toggle without a real actor identity, and this
 * mirrors the server's own routeEditor gate on the endpoint. */
export function SubscribeButton({ targetRef, canEdit }: { targetRef: string; canEdit: boolean }) {
  const [subscribed, setSubscribed] = useState<boolean | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setSubscribed(null)
    setError(null)
    if (!canEdit) return
    getSubscription(targetRef)
      .then((s) => setSubscribed(s.subscribed))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [targetRef, canEdit])

  if (!canEdit || subscribed === null) return null

  async function toggle() {
    setBusy(true)
    setError(null)
    try {
      const result = subscribed ? await unsubscribe(targetRef) : await subscribe(targetRef)
      setSubscribed(result.subscribed)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <span>
      <button onClick={() => void toggle()} disabled={busy}>
        {subscribed ? 'Unsubscribe' : 'Subscribe'}
      </button>
      {error && <span role="alert"> {error}</span>}
    </span>
  )
}
