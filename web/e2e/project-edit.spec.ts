import { test, expect } from '@playwright/test'
import { login, createProject, randomKey } from './helpers.js'

// Phase 7's headline gap, closed: plan.md §6.1 requires "Create, edit,
// archive, browse, and search projects" through the web UI — until
// now, edit and archive had no route, service method, or UI form at
// all (docs/mvp-acceptance.md row 3). This is the e2e spec that
// actually proves row 3, which is a criterion about the web UI
// specifically, not just the API layer.
test('a human can edit a project and archive/unarchive it entirely in the web UI', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Editable Project')

  await page.goto(`/projects/${key}`)
  await expect(page.locator('h1')).toContainText('Editable Project')
  await expect(page.getByText('Status: active')).toBeVisible()

  // --- edit title/description ---
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Title').fill('Renamed Project')
  await page.getByLabel('Description').fill('a new description')
  await page.getByRole('button', { name: 'Save' }).click()

  await expect(page.locator('h1')).toContainText('Renamed Project')
  await expect(page.getByText('a new description')).toBeVisible()

  // --- archive: visibility only (ADR 0021) ---
  await page.getByRole('button', { name: 'Archive' }).click()
  await expect(page.getByText('Status: archived')).toBeVisible()

  // Dropped from the default project list…
  await page.goto('/')
  await expect(page.getByRole('link', { name: 'Renamed Project' })).toHaveCount(0)

  // …but still reachable directly, and its tickets/features stay
  // fully readable/writable — archive never redirects or blocks the
  // detail page itself.
  await page.goto(`/projects/${key}`)
  await expect(page.locator('h1')).toContainText('Renamed Project')
  await expect(page.getByText('Status: archived')).toBeVisible()

  // …and present again with the "include archived" toggle.
  await page.goto('/')
  await page.getByLabel('Include archived').check()
  await expect(page.getByRole('link', { name: 'Renamed Project' })).toBeVisible()

  // --- unarchive ---
  await page.goto(`/projects/${key}`)
  await page.getByRole('button', { name: 'Unarchive' }).click()
  await expect(page.getByText('Status: active')).toBeVisible()

  await page.goto('/')
  await expect(page.getByRole('link', { name: 'Renamed Project' })).toBeVisible()
})
