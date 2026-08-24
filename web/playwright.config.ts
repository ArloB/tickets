import { defineConfig } from '@playwright/test'

const port = process.env.TICKETS_E2E_PORT ?? '18084'
export const baseURL = `http://127.0.0.1:${port}`
export const adminPassword = process.env.TICKETS_E2E_ADMIN_PASSWORD ?? 'e2e-admin-password'

// Not part of `task ci` (like `task bench`) — this drives a real
// compiled `tickets` binary against a fresh SQLite data dir, which is
// slower and needs `task build` to have run first. `task web:e2e` runs
// it explicitly.
export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  // One shared server + database across the whole run (run-server.mjs
  // creates one fresh data dir per `npx playwright test` invocation,
  // not per test) — tests must not assume an empty database, so each
  // spec creates its own uniquely-keyed project. Serial execution
  // keeps failures easy to attribute; the suite is small enough that
  // parallelizing wouldn't meaningfully speed it up yet.
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'node e2e/run-server.mjs',
    url: `${baseURL}/healthz`,
    reuseExistingServer: false,
    timeout: 20_000,
    env: {
      TICKETS_E2E_PORT: port,
      TICKETS_E2E_ADMIN_PASSWORD: adminPassword,
    },
  },
})
