import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiPost, apiLogin, apiRequest } from './helpers.js'

test('ticket board: moving a card via the status select relocates it into the target column', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Ticket Board')
  const ticket = await createTicket(page.request, key, { title: 'Board Card' })

  await page.goto(`/projects/${key}/board`)
  const card = page.locator('li.board-card', { hasText: ticket.ref })
  await expect(card).toBeVisible()
  await card.getByLabel('Move to').selectOption('in_progress')

  const inProgressColumn = page.locator('section.board-column', { has: page.getByRole('heading', { name: 'in_progress' }) })
  await expect(inProgressColumn.locator('li.board-card', { hasText: ticket.ref })).toBeVisible()
  const backlogColumn = page.locator('section.board-column', { has: page.getByRole('heading', { name: 'backlog', exact: true }) })
  await expect(backlogColumn.locator('li.board-card', { hasText: ticket.ref })).toHaveCount(0)
})

test('feature board: moving a card via the status select relocates it into the target column', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Feature Board')
  const feature = await apiPost<{ ref: string }>(page.request, `/projects/${key}/features`, {
    title: 'Board Feature',
    description: 'seeded by the e2e suite',
    priority: 'medium',
  })

  await page.goto(`/projects/${key}/features/board`)
  const card = page.locator('li.board-card', { hasText: feature.ref })
  await expect(card).toBeVisible()
  await card.getByLabel('Move to').selectOption('ready')

  const readyColumn = page.locator('section.board-column', { has: page.getByRole('heading', { name: 'ready' }) })
  await expect(readyColumn.locator('li.board-card', { hasText: feature.ref })).toBeVisible()
})

test('backlog reorder: Up/Down buttons swap same-priority rows and the order survives a reload', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Backlog Reorder')
  const first = await createTicket(page.request, key, { title: 'First', priority: 'critical' })
  const second = await createTicket(page.request, key, { title: 'Second', priority: 'critical' })

  await page.goto(`/projects/${key}/backlog`)
  const rows = () => page.locator('table tbody tr')
  await expect(rows()).toHaveCount(2)
  await expect(rows().nth(0)).toContainText(first.ref)
  await expect(rows().nth(1)).toContainText(second.ref)

  await page.getByRole('button', { name: `Move ${first.ref} down` }).click()
  await expect(rows().nth(0)).toContainText(second.ref)
  await expect(rows().nth(1)).toContainText(first.ref)

  await page.reload()
  await expect(rows()).toHaveCount(2)
  await expect(rows().nth(0)).toContainText(second.ref)
  await expect(rows().nth(1)).toContainText(first.ref)
})

test('backlog bulk actions: applies a status change to selected rows and reports per-row results', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Backlog Bulk')
  const a = await createTicket(page.request, key, { title: 'Bulk A' })
  const b = await createTicket(page.request, key, { title: 'Bulk B' })

  await page.goto(`/projects/${key}/backlog`)
  await page.getByLabel(`Select ${a.ref}`).check()
  await page.getByLabel(`Select ${b.ref}`).check()
  await page.getByLabel('Set status to').selectOption('done')
  await page.getByRole('button', { name: 'Apply to selected' }).click()

  await expect(page.getByText(`${a.ref}: done`)).toBeVisible()
  await expect(page.getByText(`${b.ref}: done`)).toBeVisible()
  const rows = page.locator('table tbody tr')
  await expect(rows.filter({ hasText: a.ref })).toContainText('done')
  await expect(rows.filter({ hasText: b.ref })).toContainText('done')
})

test('backlog bulk actions: a version-conflicted row refreshes its cached version so retrying actually works', async ({
  page,
  request,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Backlog Bulk Retry')
  const a = await createTicket(page.request, key, { title: 'Conflicted' })

  await page.goto(`/projects/${key}/backlog`)

  // Bump the ticket out from under the page's cached version 1 —
  // simulating a concurrent editor — so the first bulk apply attempt
  // is guaranteed to hit version_conflict.
  await apiLogin(request)
  await apiRequest(request, 'PUT', `/tickets/${a.ref}`, {
    type: 'task',
    title: 'Conflicted',
    description: 'seeded by the e2e suite',
    priority: 'medium',
  }, { 'If-Match': '"1"' })

  await page.getByLabel(`Select ${a.ref}`).check()
  await page.getByLabel('Set status to').selectOption('done')
  await page.getByRole('button', { name: 'Apply to selected' }).click()
  await expect(page.getByText(new RegExp(`^${a.ref}: failed`))).toBeVisible()

  // The row should still be selected for retry, and this second
  // attempt must use the refreshed version the failed attempt just
  // pulled down — not the same stale version that just failed.
  await expect(page.getByLabel(`Select ${a.ref}`)).toBeChecked()
  await page.getByRole('button', { name: 'Apply to selected' }).click()
  await expect(page.getByText(`${a.ref}: done`)).toBeVisible()
})
