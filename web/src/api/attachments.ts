import { apiFetch, apiFetchMultipart, ifMatchHeader } from './client'
import { entityPathSegment } from './refs'
import type { Attachment, AttachmentsPage, AttachmentVersionsPage } from './types'

function ownerPath(ownerRef: string | undefined, commentId: number | undefined): string {
  if (commentId !== undefined) {
    return `/comments/${commentId}/attachments`
  }
  return `/${entityPathSegment(ownerRef!)}/${encodeURIComponent(ownerRef!)}/attachments`
}

export async function listAttachments(
  ownerRef?: string,
  commentId?: number,
): Promise<AttachmentsPage> {
  return apiFetch<AttachmentsPage>(ownerPath(ownerRef, commentId))
}

export async function uploadAttachment(
  ownerRef: string | undefined,
  commentId: number | undefined,
  title: string,
  file: File,
  mediaType?: string,
): Promise<Attachment> {
  const form = new FormData()
  form.set('title', title)
  if (mediaType) form.set('media_type', mediaType)
  form.set('file', file)
  return apiFetchMultipart<Attachment>(ownerPath(ownerRef, commentId), { form })
}

export async function addPathAttachment(
  ownerRef: string | undefined,
  commentId: number | undefined,
  title: string,
  path: string,
  mediaType?: string,
): Promise<Attachment> {
  return apiFetch<Attachment>(ownerPath(ownerRef, commentId), {
    method: 'POST',
    body: { title, path, media_type: mediaType },
  })
}

export async function getAttachment(id: number): Promise<Attachment> {
  return apiFetch<Attachment>(`/attachments/${id}`)
}

export async function listAttachmentVersions(id: number): Promise<AttachmentVersionsPage> {
  return apiFetch<AttachmentVersionsPage>(`/attachments/${id}/versions`)
}

export function attachmentDownloadUrl(id: number, version?: number): string {
  return version
    ? `/api/v1/attachments/${id}/versions/${version}/download`
    : `/api/v1/attachments/${id}/download`
}

export async function replaceUploadAttachment(
  id: number,
  file: File,
  expectedVersion: number,
  mediaType?: string,
): Promise<Attachment> {
  const form = new FormData()
  if (mediaType) form.set('media_type', mediaType)
  form.set('file', file)
  return apiFetchMultipart<Attachment>(`/attachments/${id}`, {
    method: 'PUT',
    form,
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function replacePathAttachment(
  id: number,
  path: string,
  expectedVersion: number,
  mediaType?: string,
): Promise<Attachment> {
  return apiFetch<Attachment>(`/attachments/${id}`, {
    method: 'PUT',
    body: { path, media_type: mediaType },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function deleteAttachment(id: number, expectedVersion: number): Promise<void> {
  await apiFetch<void>(`/attachments/${id}`, {
    method: 'DELETE',
    headers: ifMatchHeader(expectedVersion),
  })
}
