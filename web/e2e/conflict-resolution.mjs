// Manual verification script for the Phase 4 exit criterion
// (plan.md: "a human can carry out the core workflow entirely in the
// web UI, including resolving an optimistic concurrency conflict
// without losing edits"). Not wired into `task ci` — the Milestone 5
// plan calls for a proper `task web:e2e` Playwright suite; this
// script is a manually-run starting point for that, kept here rather
// than thrown away after the manual verification pass that wrote it.
//
// Requires:
//   - A `tickets` server already running with a fresh/empty data dir
//     and `--anonymous-read` on (the default for a loopback bind), an
//     admin account created via POST /api/v1/setup, and a project
//     "ABC" with ticket "ABC-1" already created (bug, priority high,
//     severity high, title "Fix the parser", description "original
//     description").
//   - `playwright` installed (`npm install --no-save playwright`,
//     `npx playwright install chromium`) — not a project dependency,
//     since this script isn't part of the automated suite yet.
//
// Usage:
//   TICKETS_BASE_URL=http://127.0.0.1:18084 node e2e/conflict-resolution.mjs
//
// Simulates two concurrent editors of the same ticket: session A
// (browser) starts editing title+description but doesn't submit yet;
// session B (a second login, via fetch) changes title (the same field
// A is editing, to a different value — a real conflict) and priority
// (a field A never touches — should auto-apply silently) and saves
// first. Session A then submits its stale draft and must see the
// conflict banner, resolve the title conflict by keeping its own
// value, and successfully resubmit — ending with A's title, A's
// description, and B's priority all present together.

import { chromium } from 'playwright'

const base = process.env.TICKETS_BASE_URL ?? 'http://127.0.0.1:18084'
const username = process.env.TICKETS_ADMIN_USER ?? 'admin'
const password = process.env.TICKETS_ADMIN_PASSWORD ?? 'correcthorsebatterystaple'

const browser = await chromium.launch()
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
page.on('console', (msg) => {
  // The browser itself logs a failed-status network response as a
  // console error; the deliberate 409 this script triggers is
  // expected, not a bug, so it's excluded here.
  if (msg.type() === 'error' && !msg.text().includes('409')) {
    errors.push(`console.error: ${msg.text()}`)
  }
})

async function text() {
  return (await page.textContent('main')).replace(/\s+/g, ' ').trim()
}

// Session A: sign in as editor, open ABC-1, enter edit mode.
await page.goto(`${base}/login`)
await page.fill('input[autocomplete="username"]', username)
await page.fill('input[autocomplete="current-password"]', password)
await page.click('button[type="submit"]')
await page.waitForTimeout(400)

await page.goto(`${base}/tickets/ABC-1`)
await page.waitForTimeout(400)
await page.click('button:has-text("Edit")')
await page.waitForTimeout(200)
await page.fill('input[required]', "A's title change")
await page.locator('textarea').fill("A's description change")
console.log('SESSION A: form filled, not yet submitted')

// Session B: out-of-band concurrent edit via fetch, bumping the same
// ticket's version to 2.
const loginResp = await fetch(`${base}/api/v1/auth/login`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username, password }),
})
const cookie = loginResp.headers.get('set-cookie')?.split(';')[0]
const { csrf_token: csrf } = await loginResp.json()

const putResp = await fetch(`${base}/api/v1/tickets/ABC-1`, {
  method: 'PUT',
  headers: {
    'Content-Type': 'application/json',
    'If-Match': '"1"',
    'X-CSRF-Token': csrf,
    Cookie: cookie,
  },
  body: JSON.stringify({
    type: 'bug',
    title: "B's server title",
    description: 'original description',
    priority: 'low',
    severity: 'high',
  }),
})
console.log('SESSION B PUT STATUS:', putResp.status)
if (putResp.status !== 200) {
  throw new Error(`session B setup failed: ${await putResp.text()}`)
}

// Session A submits its stale draft — expect the conflict banner.
await page.click('button[type="submit"]')
await page.waitForTimeout(500)
const afterSubmit = await text()
console.log('AFTER SUBMIT:', afterSubmit)
if (!afterSubmit.includes('Someone else changed this record')) {
  throw new Error('expected conflict banner after stale submit')
}
if (!afterSubmit.includes('Fields you didn\'t touch (Priority) were updated')) {
  throw new Error('expected priority to be auto-applied silently')
}

// Resolve the title conflict by keeping session A's own change.
await page.locator('label:has-text("Your change")').locator('input[type="radio"]').click()
await page.waitForTimeout(200)
const afterResolve = await text()
if (!afterResolve.includes('All conflicts resolved')) {
  throw new Error('expected all-resolved message after picking a value')
}
if (await page.locator('button[type="submit"]').isDisabled()) {
  throw new Error('save button should be re-enabled once all conflicts are resolved')
}

// Resubmit — expect success, landing back on the read-only detail view.
await page.click('button[type="submit"]')
await page.waitForTimeout(500)
const final = await text()
console.log('FINAL:', final)

const checks = [
  ["A's title change", 'title survived'],
  ["A's description change", 'description survived'],
  ['low', "B's priority auto-applied"],
]
let ok = true
for (const [needle, label] of checks) {
  if (!final.includes(needle)) {
    console.error(`FAIL: ${label} — expected to find ${JSON.stringify(needle)}`)
    ok = false
  }
}
if (errors.length > 0) {
  console.error('FAIL: unexpected console/page errors:', JSON.stringify(errors))
  ok = false
}

await browser.close()
if (!ok) {
  process.exit(1)
}
console.log('PASS: conflict resolved without losing either session\'s work')
