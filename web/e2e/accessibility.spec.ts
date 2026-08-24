import { test, expect } from '@playwright/test'
import { AxeBuilder } from '@axe-core/playwright'
import { login, createProject, createTicket, randomKey, apiPost } from './helpers.js'

// axe-core needs a real layout/rendering engine (contrast, focus
// order) — a jsdom-based Vitest run can't compute most of what these
// rules check, so this lives in the Playwright suite against the real
// production build, not alongside the component tests.
async function assertNoViolations(page: import('@playwright/test').Page) {
  const results = await new AxeBuilder({ page }).analyze()
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([])
}

test.describe('accessibility smoke', () => {
  test('project overview', async ({ page }) => {
    await login(page)
    const key = randomKey()
    await createProject(page.request, key, 'Accessibility Project')
    await page.goto(`/projects/${key}`)
    await assertNoViolations(page)
  })

  test('backlog', async ({ page }) => {
    await login(page)
    const key = randomKey()
    await createProject(page.request, key, 'Accessibility Backlog')
    await createTicket(page.request, key, { title: 'A11y Ticket' })
    await page.goto(`/projects/${key}/backlog`)
    await assertNoViolations(page)
  })

  test('ticket board', async ({ page }) => {
    await login(page)
    const key = randomKey()
    await createProject(page.request, key, 'Accessibility Board')
    await createTicket(page.request, key, { title: 'A11y Board Ticket' })
    await page.goto(`/projects/${key}/board`)
    await assertNoViolations(page)
  })

  test('ticket detail', async ({ page }) => {
    await login(page)
    const key = randomKey()
    await createProject(page.request, key, 'Accessibility Detail')
    const ticket = await createTicket(page.request, key, { title: 'A11y Detail Ticket' })
    await page.goto(`/tickets/${ticket.ref}`)
    await assertNoViolations(page)
  })

  test('feature board', async ({ page }) => {
    await login(page)
    const key = randomKey()
    await createProject(page.request, key, 'Accessibility Feature Board')
    await apiPost(page.request, `/projects/${key}/features`, {
      title: 'A11y Feature',
      description: 'seeded by the e2e suite',
      priority: 'medium',
    })
    await page.goto(`/projects/${key}/features/board`)
    await assertNoViolations(page)
  })

  test('admin agents', async ({ page }) => {
    await login(page)
    await page.goto('/admin/agents')
    await assertNoViolations(page)
  })
})
