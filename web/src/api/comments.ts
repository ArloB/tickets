import { apiFetch, ifMatchHeader } from './client'
import type { CommentDetail, CommentsPage } from './types'

// Comments are ticket-only — no feature/decision comments exist in
// this API. A comment's version is independent of its parent ticket's
// (docs/contracts/concurrency.md): never send a ticket's If-Match here
// or vice versa.

export async function listComments(ticketRef: string): Promise<CommentsPage> {
  return apiFetch<CommentsPage>(`/tickets/${encodeURIComponent(ticketRef)}/comments`)
}

export async function getComment(commentId: number): Promise<CommentDetail> {
  return apiFetch<CommentDetail>(`/comments/${commentId}`)
}

export async function addComment(ticketRef: string, body: string): Promise<CommentDetail> {
  return apiFetch<CommentDetail>(`/tickets/${encodeURIComponent(ticketRef)}/comments`, {
    method: 'POST',
    body: { body },
  })
}

export async function editComment(
  commentId: number,
  body: string,
  expectedVersion: number,
): Promise<CommentDetail> {
  return apiFetch<CommentDetail>(`/comments/${commentId}`, {
    method: 'PATCH',
    body: { body },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function deleteComment(commentId: number, expectedVersion: number): Promise<void> {
  await apiFetch<void>(`/comments/${commentId}`, {
    method: 'DELETE',
    headers: ifMatchHeader(expectedVersion),
  })
}
