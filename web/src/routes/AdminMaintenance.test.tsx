import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminMaintenance from './AdminMaintenance'
import * as adminApi from '../api/admin'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import type { IntegrityReport, RestorePendingStatus } from '../api/types'

vi.mock('../api/admin', () => ({
  downloadBackup: vi.fn(),
  getRestoreStatus: vi.fn(),
  uploadRestore: vi.fn(),
  dismissFailedRestore: vi.fn(),
  downloadExport: vi.fn(),
  getIntegrityReport: vi.fn(),
  runGC: vi.fn(),
}))

vi.mock('../auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

const downloadBackup = vi.mocked(adminApi.downloadBackup)
const getRestoreStatus = vi.mocked(adminApi.getRestoreStatus)
const uploadRestore = vi.mocked(adminApi.uploadRestore)
const dismissFailedRestore = vi.mocked(adminApi.dismissFailedRestore)
const downloadExport = vi.mocked(adminApi.downloadExport)
const getIntegrityReport = vi.mocked(adminApi.getIntegrityReport)
const runGC = vi.mocked(adminApi.runGC)
const mockUseAuth = vi.mocked(useAuth)

function cleanReport(): IntegrityReport {
  return { database_ok: true, foreign_key_violations: [], corrupted_blobs: [], orphaned_blobs: [] }
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/admin/maintenance']}>
      <Routes>
        <Route path="/admin/maintenance" element={<AdminMaintenance />} />
        <Route path="/" element={<p>Home</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockUseAuth.mockReturnValue({
    me: { permission: 'editor', is_admin: true, actor: 'human:admin' },
    ready: true,
    bootstrapError: null,
    login: vi.fn(),
    logout: vi.fn(),
  })
  getRestoreStatus.mockResolvedValue({ pending: false, failed: false })
  URL.createObjectURL = vi.fn(() => 'blob:mock')
  URL.revokeObjectURL = vi.fn()
})

describe('AdminMaintenance', () => {
  it('redirects a non-admin away instead of rendering the page', async () => {
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: false, actor: 'human:bob' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    renderPage()
    expect(await screen.findByText('Home')).toBeInTheDocument()
  })

  it('downloads a backup when the button is clicked', async () => {
    const blob = new Blob(['zip bytes'])
    downloadBackup.mockResolvedValue(blob)
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Download backup' }))

    await waitFor(() => expect(downloadBackup).toHaveBeenCalled())
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob)
  })

  it('shows a pending-restore notice once a restore is staged', async () => {
    getRestoreStatus.mockResolvedValue({ pending: true, failed: false } as RestorePendingStatus)
    renderPage()
    expect(await screen.findByText(/restart the server to apply it/i)).toBeInTheDocument()
  })

  it('shows and can dismiss a failed-restore notice', async () => {
    getRestoreStatus.mockResolvedValueOnce({ pending: false, failed: true, last_error: 'boom' })
    dismissFailedRestore.mockResolvedValue(undefined)
    getRestoreStatus.mockResolvedValueOnce({ pending: false, failed: false })
    renderPage()

    expect(await screen.findByText(/boom/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => expect(dismissFailedRestore).toHaveBeenCalled())
  })

  it('uploads a selected file to stage a restore', async () => {
    uploadRestore.mockResolvedValue({ staged: true })
    renderPage()

    const file = new File(['zip bytes'], 'backup.zip', { type: 'application/zip' })
    const input = await screen.findByLabelText('Backup archive')
    await userEvent.upload(input, file)
    await userEvent.click(screen.getByRole('button', { name: 'Upload and stage' }))

    await waitFor(() => expect(uploadRestore).toHaveBeenCalledWith(file))
  })

  it('downloads an export, passing the attachments flag through', async () => {
    downloadExport.mockResolvedValue(new Blob(['{}']))
    renderPage()

    await userEvent.click(await screen.findByLabelText('Include attachment files'))
    await userEvent.click(screen.getByRole('button', { name: 'Download export' }))

    await waitFor(() => expect(downloadExport).toHaveBeenCalledWith(true))
  })

  it('runs an integrity check and shows the report', async () => {
    getIntegrityReport.mockResolvedValue(cleanReport())
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Run integrity check' }))

    expect(await screen.findByText('Database')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove orphaned blobs' })).not.toBeInTheDocument()
  })

  it('requires a confirm step before removing orphaned blobs', async () => {
    getIntegrityReport.mockResolvedValue({ ...cleanReport(), orphaned_blobs: ['abc123'] })
    runGC.mockResolvedValue(cleanReport())
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Run integrity check' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Remove orphaned blobs' }))

    expect(runGC).not.toHaveBeenCalled()
    expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(runGC).toHaveBeenCalled())
  })

  it('shows an error message when a download fails', async () => {
    downloadBackup.mockRejectedValue(new ApiError(
      { code: 'internal_error', message: 'disk full', field: null, correlation_id: '', current_version: null },
      500,
    ))
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Download backup' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('disk full')
  })
})
