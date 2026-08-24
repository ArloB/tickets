import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey } from './helpers.js'

// Phase 4's scope (plan.md §"core web UI") explicitly names responsive
// layout. No breakpoint-specific redesign exists — the check here is
// the baseline every view must clear: nothing forces the whole page to
// scroll horizontally at a narrow viewport width. A wide element that
// needs its own horizontal scroll (the kanban board, a data table) is
// fine as long as that scroll is contained to itself via
// `overflow-x: auto` rather than blowing out <html>'s own width.
test('no view forces page-level horizontal scroll at a narrow (320px) viewport', async ({
  browser,
}) => {
  const context = await browser.newContext({ viewport: { width: 320, height: 667 } })
  const page = await context.newPage()
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Responsive Check')
  const ticket = await createTicket(page.request, key, { title: 'Responsive Ticket' })

  async function assertNoHorizontalOverflow(label: string, url: string) {
    await page.goto(url)
    await page.waitForTimeout(150)
    const { scrollWidth, clientWidth } = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }))
    expect(scrollWidth, `${label}: page scrollWidth vs clientWidth`).toBeLessThanOrEqual(clientWidth)
  }

  await assertNoHorizontalOverflow('project list', '/')
  await assertNoHorizontalOverflow('project overview', `/projects/${key}`)
  await assertNoHorizontalOverflow('backlog', `/projects/${key}/backlog`)
  await assertNoHorizontalOverflow('ticket board', `/projects/${key}/board`)
  await assertNoHorizontalOverflow('feature board', `/projects/${key}/features/board`)
  await assertNoHorizontalOverflow('ticket detail', `/tickets/${ticket.ref}`)
  await assertNoHorizontalOverflow('admin agents', '/admin/agents')
  await assertNoHorizontalOverflow('decision register', `/projects/${key}/decisions`)
})
