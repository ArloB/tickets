import { apiFetch } from './client'
import { entityPathSegment } from './refs'
import type { Subscription } from './types'

export async function getSubscription(ref: string): Promise<Subscription> {
  return apiFetch<Subscription>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/subscribe`)
}

export async function subscribe(ref: string): Promise<Subscription> {
  return apiFetch<Subscription>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/subscribe`, { method: 'POST' })
}

export async function unsubscribe(ref: string): Promise<Subscription> {
  return apiFetch<Subscription>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/subscribe`, { method: 'DELETE' })
}
