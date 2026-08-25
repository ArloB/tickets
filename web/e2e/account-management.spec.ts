import { test, expect } from '@playwright/test'
import { login } from './helpers.js'

// Phase 7: human account management was entirely missing through
// Phase 6 (docs/troubleshooting.md) — no way to create a second
// account, no password-change action at all. This proves both through
// the actual web UI: an admin creates a second account and resets a
// user's password, and that user changes their own password and can
// log back in with the new one.
test('an admin can create an account and reset its password; the user can change their own', async ({
  page,
  browser,
}) => {
  await login(page)
  await page.goto('/admin/accounts')

  const username = `e2euser${Math.floor(Math.random() * 100000)}`
  await page.getByLabel('Username').fill(username)
  await page.getByLabel('Password', { exact: true }).fill('original-password-123')
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByText(username)).toBeVisible()

  // The new user logs in and changes their own password.
  const userContext = await browser.newContext()
  const userPage = await userContext.newPage()
  await login(userPage, username, 'original-password-123')
  await userPage.goto('/admin/accounts')
  await userPage.getByLabel('Old password').fill('original-password-123')
  await userPage.getByLabel('New password').fill('self-changed-password-456')
  await userPage.getByRole('button', { name: 'Change password' }).click()
  await expect(userPage.getByText('Password changed.')).toBeVisible()
  await userContext.close()

  // The new password actually works; the old one no longer does.
  const verifyContext = await browser.newContext()
  const verifyPage = await verifyContext.newPage()
  await login(verifyPage, username, 'self-changed-password-456')
  await expect(verifyPage).toHaveURL('/')
  await verifyContext.close()

  // Admin reset: the admin resets it back to something known, without
  // ever supplying the current password.
  await page.reload()
  const row = page.locator('li', { hasText: username })
  await row.getByRole('button', { name: 'Reset password' }).click()
  await row.getByLabel('New password').fill('admin-reset-password-789')
  await row.getByRole('button', { name: 'Change password' }).click()
  await expect(row.getByText('Password changed.')).toBeVisible()
})
