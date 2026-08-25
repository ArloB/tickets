import { apiFetch, ifMatchHeader } from './client'
import { entityPathSegment } from './refs'
import type { CommentDetail, CommentsPage } from './types'

// Comments exist on any of the six principal kinds §5.10 names (Phase
// 6 Step 2) — ref may be a ticket/feature/decision/plan/document
// reference or a bare project key; entityPathSegment picks the right
// URL prefix. A comment's version is independent of its parent's
// (docs/contracts/concurrency.md): never send the parent's If-Match
// here or vice versa.

export async function listComments(ref: string): Promise<CommentsPage> {
  return apiFetch<CommentsPage>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/comments`)
}

export async function getComment(commentId: number): Promise<CommentDetail> {
  return apiFetch<CommentDetail>(`/comments/${commentId}`)
}

export async function addComment(ref: string, body: string): Promise<CommentDetail> {
  return apiFetch<CommentDetail>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/comments`, {
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
