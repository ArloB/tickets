import { test, expect } from '@playwright/test'
import { login, randomKey } from './helpers.js'

test('admin agent/token management: create an agent, issue a token shown once, then revoke it', async ({ page }) => {
  await login(page)

  await expect(page.getByRole('link', { name: 'Agents' })).toBeVisible()
  await page.getByRole('link', { name: 'Agents' }).click()
  await page.waitForURL('**/admin/agents')

  const name = randomKey('ci').toLowerCase()
  await page.getByLabel('Name').fill(name)
  await page.getByLabel('Description').fill('created by the e2e suite')
  await page.getByRole('button', { name: 'Create agent' }).click()

  const agentToggle = page.getByRole('button', { name })
  await expect(agentToggle).toBeVisible()
  await agentToggle.click()
  await expect(page.getByText('No tokens yet.')).toBeVisible()

  const tokenForm = page.locator('form:has(button:has-text("Create token"))')
  await tokenForm.locator('input').first().fill('deploy key')
  await tokenForm.getByRole('button', { name: 'Create token' }).click()

  // ADR 0004: the raw value is shown exactly once, right here.
  await expect(page.getByText('This token will not be shown again.')).toBeVisible()
  const rawTokenInput = page.locator('div[role="alert"] input[readonly]')
  const rawToken = await rawTokenInput.inputValue()
  expect(rawToken.length).toBeGreaterThan(10)

  await page.getByRole('button', { name: 'Dismiss' }).click()
  await expect(page.getByText('This token will not be shown again.')).toHaveCount(0)
  await expect(page.getByText('deploy key')).toBeVisible()

  await page.getByRole('button', { name: 'Revoke' }).click()
  await expect(page.getByText(/revoked/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Revoke' })).toHaveCount(0)
})

test('the agent list is enforced server-side, not just hidden by nav gating', async ({ request }) => {
  // The seeded admin is the only account this suite creates — there's
  // no separate non-admin human login to test with, so this asserts
  // the server-side enforcement directly: an unauthenticated GET
  // (viewer permission, never admin) must 403, proving the UI's nav
  // gating isn't the only thing standing between a non-admin and this
  // data.
  const resp = await request.get('/api/v1/agents')
  expect(resp.status()).toBe(403)
})
