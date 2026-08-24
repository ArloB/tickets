import { test, expect } from '@playwright/test'
import { login, createProject, randomKey } from './helpers.js'

test('create a document as a path reference and confirm the server never reads it', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Content Representations')

  await page.goto(`/projects/${key}/documents`)
  await page.getByRole('button', { name: 'New document' }).click()
  await page.getByLabel('Title').fill('External spec')
  await page.getByRole('radio', { name: 'path', exact: true }).check()
  await page.getByRole('textbox', { name: 'Path' }).fill('/etc/passwd')
  await page.getByRole('button', { name: 'Create document' }).click()

  const docLink = page.getByRole('link', { name: 'External spec' })
  await expect(docLink).toBeVisible()
  await docLink.click()

  await expect(page.getByRole('heading', { level: 1 })).toContainText('External spec')
  await expect(page.getByText('/etc/passwd')).toBeVisible()

  const listResp = await page.request.get(`/api/v1/projects/${key}/documents`)
  const list = (await listResp.json()) as { items: { ref: string }[] }
  const ref = list.items[0].ref

  const downloadResp = await page.request.get(`/api/v1/documents/${ref}/download`)
  expect(downloadResp.ok()).toBe(false)
  const body = await downloadResp.text()
  expect(body).not.toContain('root:')
})

test('create a plan as a file upload, download it, then replace it with a new version', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Content Representations')

  await page.goto(`/projects/${key}/plans`)
  await page.getByRole('button', { name: 'New plan' }).click()
  await page.getByLabel('Title').fill('Rollout plan')
  await page.getByRole('radio', { name: 'file', exact: true }).check()
  await page.setInputFiles('input[type="file"]', {
    name: 'plan.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('plan v1'),
  })
  await page.getByRole('button', { name: 'Create plan' }).click()

  const planLink = page.getByRole('link', { name: 'Rollout plan' })
  await expect(planLink).toBeVisible()
  await planLink.click()

  const downloadLink = page.getByRole('link', { name: 'plan.txt' })
  await expect(downloadLink).toBeVisible()
  const downloadUrl = await downloadLink.getAttribute('href')
  const downloadResp = await page.request.get(downloadUrl!)
  expect(await downloadResp.text()).toBe('plan v1')

  await page.getByRole('button', { name: 'Edit' }).click()
  await page.setInputFiles('input[type="file"]', {
    name: 'plan-v2.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('plan v2'),
  })
  await page.getByRole('button', { name: 'Save' }).click()

  const downloadLink2 = page.getByRole('link', { name: 'plan-v2.txt' })
  await expect(downloadLink2).toBeVisible()
  const downloadUrl2 = await downloadLink2.getAttribute('href')
  const downloadResp2 = await page.request.get(downloadUrl2!)
  expect(await downloadResp2.text()).toBe('plan v2')
})
