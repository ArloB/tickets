import { test, expect } from '@playwright/test'
import { login, randomKey } from './helpers.js'

test('golden path: sign in, create project, feature, ticket, and comment', async ({ page }) => {
  await login(page)

  const key = randomKey()
  await page.goto('/')
  await page.getByRole('button', { name: 'New project' }).click()
  await page.getByLabel('Key').fill(key)
  await page.getByLabel('Title').fill('Smoke Test Project')
  await page.getByRole('button', { name: 'Create project' }).click()
  await page.waitForURL(`**/projects/${key}`)

  await page.getByRole('button', { name: 'New feature' }).click()
  await page.getByLabel('Title').fill('Smoke Test Feature')
  await page.getByRole('button', { name: 'Create feature' }).click()
  await expect(page.getByText('Smoke Test Feature')).toBeVisible()

  await page.getByRole('link', { name: 'View backlog' }).click()
  await page.waitForURL(`**/projects/${key}/backlog`)
  await page.getByRole('button', { name: 'New ticket' }).click()
  const featureSelect = page.getByLabel('Feature')
  const featureOption = featureSelect.locator('option', { hasText: 'Smoke Test Feature' })
  await expect(featureOption).toHaveCount(1)
  await featureSelect.selectOption(await featureOption.getAttribute('value'))
  await page.getByLabel('Title').fill('Smoke Test Ticket')
  await page.getByLabel('Description').fill('created by the golden-path smoke test')
  await page.getByRole('button', { name: 'Create ticket' }).click()

  const ticketLink = page.getByRole('link', { name: new RegExp(`^${key}-\\d+$`) })
  await expect(ticketLink).toBeVisible()
  await ticketLink.click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Smoke Test Ticket')

  await page.getByPlaceholder('Add a comment…').fill('a smoke-test comment')
  await page.getByRole('button', { name: 'Add comment' }).click()
  await expect(page.getByText('a smoke-test comment')).toBeVisible()
})

test('anonymous read-only mode: can browse but has no mutating controls, and the server rejects a forged mutation', async ({
  browser,
}) => {
  // A fresh, unauthenticated context — no cookies, no CSRF token.
  const anonContext = await browser.newContext()
  const page = await anonContext.newPage()

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Projects' })).toBeVisible()
  // Anonymous viewer mode: read works, but nothing that mutates is offered.
  await expect(page.getByRole('button', { name: 'New project' })).toHaveCount(0)

  const resp = await page.request.post('/api/v1/projects', {
    data: { key: randomKey(), title: 'Should be rejected', description: '' },
  })
  expect(resp.status()).toBe(403)

  await anonContext.close()
})
