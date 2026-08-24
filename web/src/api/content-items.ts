import { apiFetch, apiFetchMultipart, ifMatchHeader } from './client'
import type {
  ContentItemDetail,
  ContentItemDiff,
  ContentItemRepresentation,
  ContentItemsPage,
  ContentItemVersionsPage,
} from './types'

/** "plans" or "documents" — the URL segment for a given content item
 * kind (internal/httpapi/server.go registers both as separate route
 * trees over the same handlers). */
export type ContentItemUrlKind = 'plans' | 'documents'

/** Display copy for each urlKind — the one place ContentLibrary.tsx and
 * ContentItemDetail.tsx both read "plan"/"Plans" vs "document"/
 * "Documents" from, instead of each carrying its own
 * `kind === 'plans' ? ... : ...` ternary. */
export const CONTENT_ITEM_LABELS: Record<ContentItemUrlKind, { singular: string; plural: string }> = {
  plans: { singular: 'plan', plural: 'Plans' },
  documents: { singular: 'document', plural: 'Documents' },
}

export async function listContentItems(
  urlKind: ContentItemUrlKind,
  projectKey: string,
  cursor?: string,
): Promise<ContentItemsPage> {
  const params = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
  return apiFetch<ContentItemsPage>(`/projects/${encodeURIComponent(projectKey)}/${urlKind}${params}`)
}

export async function getContentItem(urlKind: ContentItemUrlKind, ref: string): Promise<ContentItemDetail> {
  return apiFetch<ContentItemDetail>(`/${urlKind}/${encodeURIComponent(ref)}`)
}

export interface CreateContentItemInput {
  title: string
  representation?: ContentItemRepresentation
  body?: string
  path?: string
  url?: string
}

export async function createContentItem(
  urlKind: ContentItemUrlKind,
  projectKey: string,
  input: CreateContentItemInput,
): Promise<ContentItemDetail> {
  return apiFetch<ContentItemDetail>(`/projects/${encodeURIComponent(projectKey)}/${urlKind}`, {
    method: 'POST',
    body: input,
  })
}

export async function uploadContentItem(
  urlKind: ContentItemUrlKind,
  projectKey: string,
  title: string,
  file: File,
  mediaType?: string,
): Promise<ContentItemDetail> {
  const form = new FormData()
  form.set('title', title)
  if (mediaType) form.set('media_type', mediaType)
  form.set('file', file)
  return apiFetchMultipart<ContentItemDetail>(`/projects/${encodeURIComponent(projectKey)}/${urlKind}`, { form })
}

export interface UpdateContentItemInput {
  title: string
  body?: string
  path?: string
  url?: string
}

/** PATCH /plans|documents/{ref} is a full-representation update (like
 * PATCH /decisions/{ref}) — title plus whichever field matches the
 * item's own (immutable) representation. Requires If-Match; a 409
 * carries the live record's current_version (docs/contracts/
 * errors.md). */
export async function updateContentItem(
  urlKind: ContentItemUrlKind,
  ref: string,
  input: UpdateContentItemInput,
  expectedVersion: number,
): Promise<ContentItemDetail> {
  return apiFetch<ContentItemDetail>(`/${urlKind}/${encodeURIComponent(ref)}`, {
    method: 'PATCH',
    body: input,
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function replaceContentItemFile(
  urlKind: ContentItemUrlKind,
  ref: string,
  title: string,
  file: File,
  expectedVersion: number,
  mediaType?: string,
): Promise<ContentItemDetail> {
  const form = new FormData()
  form.set('title', title)
  if (mediaType) form.set('media_type', mediaType)
  form.set('file', file)
  return apiFetchMultipart<ContentItemDetail>(`/${urlKind}/${encodeURIComponent(ref)}`, {
    method: 'PATCH',
    form,
    headers: ifMatchHeader(expectedVersion),
  })
}

export function contentItemDownloadUrl(urlKind: ContentItemUrlKind, ref: string, version?: number): string {
  return version
    ? `/api/v1/${urlKind}/${encodeURIComponent(ref)}/versions/${version}/download`
    : `/api/v1/${urlKind}/${encodeURIComponent(ref)}/download`
}

/** GET /plans|documents/{ref}/versions — archived prior states, oldest
 * first. Does not include the live state. */
export async function listContentItemVersions(
  urlKind: ContentItemUrlKind,
  ref: string,
): Promise<ContentItemVersionsPage> {
  return apiFetch<ContentItemVersionsPage>(`/${urlKind}/${encodeURIComponent(ref)}/versions`)
}

/** GET /plans|documents/{ref}/diff?from=&to= — a line-level diff of
 * title and body between two named versions, either of which may be
 * the live version. */
export async function getContentItemDiff(
  urlKind: ContentItemUrlKind,
  ref: string,
  from: number,
  to: number,
): Promise<ContentItemDiff> {
  return apiFetch<ContentItemDiff>(`/${urlKind}/${encodeURIComponent(ref)}/diff?from=${from}&to=${to}`)
}
