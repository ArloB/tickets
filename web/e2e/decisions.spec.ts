import { test, expect } from '@playwright/test'
import { login, createProject, randomKey } from './helpers.js'

// Phase 6 Step 11: docs/mvp-acceptance.md row 3 found no e2e spec
// created or edited a decision through the web UI (search.spec.ts
// only ever created one via a raw API POST). This closes that gap —
// mirrors content-item-representations.spec.ts's create-then-edit
// shape for the other record kind row 3 flagged.
test('create a decision through the UI, then edit it', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Decisions')

  await page.goto(`/projects/${key}/decisions`)
  await page.getByRole('button', { name: 'New decision' }).click()
  await page.getByLabel('Title').fill('Use SQLite')
  await page.getByLabel('Context').fill('Need an embedded database')
  await page.getByLabel('Decision', { exact: true }).fill('Use SQLite in WAL mode')
  await page.getByLabel('Rationale').fill('No separate server process to run')
  await page.getByRole('button', { name: 'Create decision' }).click()

  const decisionLink = page.getByRole('link', { name: 'Use SQLite' })
  await expect(decisionLink).toBeVisible()
  await decisionLink.click()

  await expect(page.getByRole('heading', { level: 1 })).toContainText('Use SQLite')
  await expect(page.getByText('Use SQLite in WAL mode')).toBeVisible()

  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Rationale').fill('No separate server process to run; one file to back up')
  await page.getByLabel('Status').selectOption('accepted')
  await page.getByRole('button', { name: 'Save' }).click()

  await expect(page.getByText('No separate server process to run; one file to back up')).toBeVisible()
  await expect(page.getByText(/Project: .* · accepted/)).toBeVisible()
})
