// Spins up a real `tickets` server against a fresh, throwaway data
// directory for the Playwright suite. Playwright's webServer.command
// runs this script and waits for TICKETS_E2E_URL + "/healthz" to
// respond before running any test (playwright.config.ts); teardown
// just kills this process, so the child server must die with it.
//
// Two-step because `tickets server` has no "create the first admin
// and start" flag: `tickets setup` provisions the admin account against
// the data dir, then `tickets server` is exec'd against that same dir.
import { spawn, spawnSync } from 'node:child_process'
import { mkdtempSync, readdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

const repoRoot = path.resolve(import.meta.dirname, '..', '..')
const bin = path.join(repoRoot, 'bin', process.platform === 'win32' ? 'tickets.exe' : 'tickets')

const host = '127.0.0.1'
const port = process.env.TICKETS_E2E_PORT ?? '18084'
const username = 'admin'
const password = process.env.TICKETS_E2E_ADMIN_PASSWORD ?? 'e2e-admin-password'

// On Windows, Playwright tears down webServer via a forceful kill of
// the whole process tree rather than a signal this script's handlers
// below ever see — so `cleanup()` on a normal exit is real, but it's
// not the only line of defense. Sweep any dirs a prior run left behind
// before creating this run's own, so leftovers don't accumulate
// silently across many suite runs.
for (const entry of readdirSync(tmpdir(), { withFileTypes: true })) {
  if (entry.isDirectory() && entry.name.startsWith('tickets-e2e-')) {
    try {
      rmSync(path.join(tmpdir(), entry.name), { recursive: true, force: true })
    } catch {
      // still in use by another concurrent run, or otherwise locked — skip it.
    }
  }
}

const dataDir = mkdtempSync(path.join(tmpdir(), 'tickets-e2e-'))
function cleanup() {
  try {
    rmSync(dataDir, { recursive: true, force: true })
  } catch {
    // best-effort — see the sweep above for the actual backstop.
  }
}

const setup = spawnSync(bin, ['setup', '--data-dir', dataDir, '--username', username, '--password', password], {
  stdio: 'inherit',
})
if (setup.status !== 0) {
  cleanup()
  process.exit(setup.status ?? 1)
}

const server = spawn(bin, ['server', '--data-dir', dataDir, '--host', host, '--port', port], {
  stdio: 'inherit',
})

let shuttingDown = false
function shutdown() {
  if (shuttingDown) return
  shuttingDown = true
  server.kill()
}
process.on('SIGTERM', shutdown)
process.on('SIGINT', shutdown)

server.on('exit', (code) => {
  cleanup()
  process.exit(code ?? 0)
})
