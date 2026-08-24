import { apiFetch } from './client'
import type { NotificationsPage } from './types'

export interface ListNotificationsOptions {
  unread?: boolean
  cursor?: string
}

export async function listNotifications(opts: ListNotificationsOptions = {}): Promise<NotificationsPage> {
  const params = new URLSearchParams()
  if (opts.unread) params.set('unread', 'true')
  if (opts.cursor) params.set('cursor', opts.cursor)
  const query = params.toString() ? `?${params.toString()}` : ''
  return apiFetch<NotificationsPage>(`/notifications${query}`)
}

export async function markNotificationsRead(ids: number[]): Promise<{ marked: number }> {
  return apiFetch<{ marked: number }>('/notifications/read', { method: 'POST', body: { ids } })
}

export async function markAllNotificationsRead(): Promise<{ marked: number }> {
  return apiFetch<{ marked: number }>('/notifications/read', { method: 'POST', body: { all: true } })
}
