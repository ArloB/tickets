import { apiFetch, apiFetchBlob, apiFetchMultipart, apiFetchRaw } from './client'
import type { ImportReport, IntegrityReport, RestorePendingStatus, RestoreStagedResponse } from './types'

export async function downloadBackup(): Promise<Blob> {
  return apiFetchBlob('/admin/backup')
}

export async function getRestoreStatus(): Promise<RestorePendingStatus> {
  return apiFetch<RestorePendingStatus>('/admin/restore')
}

export async function uploadRestore(file: File): Promise<RestoreStagedResponse> {
  return apiFetchRaw<RestoreStagedResponse>('/admin/restore', {
    method: 'POST',
    body: file,
    contentType: 'application/zip',
  })
}

export async function dismissFailedRestore(): Promise<void> {
  await apiFetch<void>('/admin/restore', { method: 'DELETE' })
}

export async function downloadExport(includeAttachments: boolean): Promise<Blob> {
  return apiFetchBlob('/admin/export', {
    query: includeAttachments ? { attachments: 'true' } : undefined,
  })
}

export async function getIntegrityReport(): Promise<IntegrityReport> {
  return apiFetch<IntegrityReport>('/admin/integrity')
}

export async function runGC(): Promise<IntegrityReport> {
  return apiFetch<IntegrityReport>('/admin/integrity/gc', { method: 'POST', body: { confirm: true } })
}

export async function setupImport(envelope: File, attachments?: File): Promise<ImportReport> {
  const form = new FormData()
  form.append('envelope', envelope)
  if (attachments) {
    form.append('attachments', attachments)
  }
  return apiFetchMultipart<ImportReport>('/setup/import', { form })
}
