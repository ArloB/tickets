import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import {
  dismissFailedRestore,
  downloadBackup,
  downloadExport,
  getIntegrityReport,
  getRestoreStatus,
  runGC,
  uploadRestore,
} from '../api/admin'
import { ApiError } from '../api/client'
import type { IntegrityReport, RestorePendingStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function BackupSection() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function download() {
    setBusy(true)
    setError(null)
    try {
      const blob = await downloadBackup()
      triggerDownload(blob, `tickets-backup-${new Date().toISOString().slice(0, 10)}.zip`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section>
      <h2>Backup</h2>
      <p>
        Download a full snapshot of the database and every attachment, for disaster recovery on this
        same server.
      </p>
      {error && <p role="alert">{error}</p>}
      <button type="button" onClick={() => void download()} disabled={busy}>
        {busy ? 'Preparing backup…' : 'Download backup'}
      </button>
    </section>
  )
}

function RestoreSection() {
  const [status, setStatus] = useState<RestorePendingStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const [file, setFile] = useState<File | null>(null)

  function refresh() {
    getRestoreStatus()
      .then(setStatus)
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(refresh, [])

  async function upload() {
    if (!file) return
    setUploading(true)
    setError(null)
    try {
      await uploadRestore(file)
      setFile(null)
      refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setUploading(false)
    }
  }

  async function dismiss() {
    setError(null)
    try {
      await dismissFailedRestore()
      refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    }
  }

  return (
    <section>
      <h2>Restore</h2>
      <p>
        Upload a backup archive to stage it for restore. The upload is verified immediately, but the
        actual restore only happens the next time the server restarts — it cannot safely replace its
        own open database while running.
      </p>
      {error && <p role="alert">{error}</p>}
      {status?.pending && (
        <p role="status">A restore is staged. Restart the server to apply it.</p>
      )}
      {status?.failed && (
        <p role="alert">
          The last staged restore failed to apply: {status.last_error}. The server started normally
          with its data from before the restore attempt.{' '}
          <button type="button" onClick={() => void dismiss()}>
            Dismiss
          </button>
        </p>
      )}
      <input
        type="file"
        accept=".zip"
        onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        aria-label="Backup archive"
      />
      <button type="button" onClick={() => void upload()} disabled={!file || uploading}>
        {uploading ? 'Uploading…' : 'Upload and stage'}
      </button>
    </section>
  )
}

function ExportSection() {
  const [includeAttachments, setIncludeAttachments] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function download() {
    setBusy(true)
    setError(null)
    try {
      const blob = await downloadExport(includeAttachments)
      const ext = includeAttachments ? 'zip' : 'json'
      triggerDownload(blob, `tickets-export-${new Date().toISOString().slice(0, 10)}.${ext}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section>
      <h2>Export</h2>
      <p>
        Download a portable copy of every project, ticket, feature, decision, and document — for
        moving content to a different server, or archiving it outside Tickets entirely.
      </p>
      {error && <p role="alert">{error}</p>}
      <label>
        <input
          type="checkbox"
          checked={includeAttachments}
          onChange={(e) => setIncludeAttachments(e.target.checked)}
        />
        Include attachment files
      </label>
      <button type="button" onClick={() => void download()} disabled={busy}>
        {busy ? 'Preparing export…' : 'Download export'}
      </button>
    </section>
  )
}

function IntegrityReportView({ report }: { report: IntegrityReport }) {
  return (
    <dl>
      <dt>Database</dt>
      <dd>{report.database_ok ? 'ok' : `problems found: ${(report.database_messages ?? []).join('; ')}`}</dd>
      <dt>Foreign keys</dt>
      <dd>
        {report.foreign_key_violations.length === 0
          ? 'ok'
          : `${report.foreign_key_violations.length} violation(s)`}
      </dd>
      <dt>Blob checksums</dt>
      <dd>
        {report.corrupted_blobs.length === 0 ? 'ok' : `${report.corrupted_blobs.length} corrupted`}
      </dd>
      <dt>Orphaned blobs</dt>
      <dd>{report.orphaned_blobs.length}</dd>
    </dl>
  )
}

function IntegritySection() {
  const [report, setReport] = useState<IntegrityReport | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [checking, setChecking] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [gcRunning, setGcRunning] = useState(false)

  async function check() {
    setChecking(true)
    setError(null)
    try {
      setReport(await getIntegrityReport())
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setChecking(false)
    }
  }

  async function removeOrphans() {
    setGcRunning(true)
    setError(null)
    try {
      setReport(await runGC())
      setConfirming(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setGcRunning(false)
    }
  }

  return (
    <section>
      <h2>Integrity &amp; cleanup</h2>
      <p>
        Check the database and blob store for problems, and find blobs no attachment references
        anymore.
      </p>
      {error && <p role="alert">{error}</p>}
      <button type="button" onClick={() => void check()} disabled={checking}>
        {checking ? 'Checking…' : 'Run integrity check'}
      </button>
      {report && <IntegrityReportView report={report} />}
      {report && report.orphaned_blobs.length > 0 && !confirming && (
        <button type="button" onClick={() => setConfirming(true)}>
          Remove orphaned blobs
        </button>
      )}
      {confirming && (
        <p role="alert">
          This permanently deletes {report?.orphaned_blobs.length} orphaned blob(s). This cannot be
          undone.{' '}
          <button type="button" onClick={() => void removeOrphans()} disabled={gcRunning}>
            {gcRunning ? 'Removing…' : 'Remove'}
          </button>{' '}
          <button type="button" onClick={() => setConfirming(false)} disabled={gcRunning}>
            Cancel
          </button>
        </p>
      )}
    </section>
  )
}

export default function AdminMaintenance() {
  const { me } = useAuth()
  if (!me?.is_admin) return <Navigate to="/" replace />

  return (
    <main>
      <h1>Maintenance</h1>
      <BackupSection />
      <RestoreSection />
      <ExportSection />
      <IntegritySection />
    </main>
  )
}
