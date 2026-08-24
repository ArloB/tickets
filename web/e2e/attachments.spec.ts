import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey } from './helpers.js'

test('upload an attachment, download it, add a path reference, and confirm the path is never served', async ({
  page,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Attachments')
  const ticket = await createTicket(page.request, key, { title: 'Ticket with attachments' })

  await page.goto(`/tickets/${ticket.ref}`)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Ticket with attachments')

  await page.getByLabel('Attachment title').fill('design notes')
  await page.setInputFiles('input[type="file"]', {
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('hello from e2e'),
  })
  await page.getByRole('button', { name: 'Add attachment' }).click()

  const attachmentLink = page.getByRole('link', { name: 'design notes' })
  await expect(attachmentLink).toBeVisible()
  await expect(page.getByText('— v1')).toBeVisible()

  const downloadUrl = await attachmentLink.getAttribute('href')
  expect(downloadUrl).toBeTruthy()
  const downloadResp = await page.request.get(downloadUrl!)
  expect(downloadResp.ok()).toBe(true)
  expect(await downloadResp.text()).toBe('hello from e2e')

  await page.getByLabel('Attachment title').fill('deploy script')
  await page.getByLabel('Reference a path').check()
  await page.getByPlaceholder('/path/to/file').fill('/etc/passwd')
  await page.getByRole('button', { name: 'Add attachment' }).click()

  const pathEntry = page.getByText('deploy script (/etc/passwd)')
  await expect(pathEntry).toBeVisible()

  const listResp = await page.request.get(`/api/v1/tickets/${ticket.ref}/attachments`)
  const list = (await listResp.json()) as { attachments: { id: number; kind: string }[] }
  const pathAttachment = list.attachments.find((a) => a.kind === 'path')
  expect(pathAttachment).toBeTruthy()

  const pathDownloadResp = await page.request.get(`/api/v1/attachments/${pathAttachment!.id}/download`)
  expect(pathDownloadResp.ok()).toBe(false)
  const body = await pathDownloadResp.text()
  expect(body).not.toContain('root:')
})
