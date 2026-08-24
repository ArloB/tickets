import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiPost } from './helpers.js'

test('search finds a ticket, a decision, and a comment, each linking to the right ref', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Search')

  const ticket = await createTicket(page.request, key, { title: 'Reticulate the splines' })
  await apiPost(page.request, `/projects/${key}/decisions`, {
    title: 'Spline policy',
    decision: 'we reticulate quadratically now',
  })
  await apiPost(page.request, `/tickets/${ticket.ref}/comments`, {
    body: 'reticulation looks fixed on my end',
  })

  await page.goto('/search?q=reticulate')

  const ticketLink = page.getByRole('link', { name: ticket.ref }).first()
  await expect(ticketLink).toBeVisible()
  await expect(page.getByText('Reticulate the splines', { exact: true })).toBeVisible()
  await expect(page.getByText('Spline policy')).toBeVisible()
  await expect(page.getByText('reticulation looks fixed on my end')).toBeVisible()

  await ticketLink.click()
  await expect(page).toHaveURL(new RegExp(`/tickets/${ticket.ref}$`))
})

test('the nav search box navigates to /search with the typed query', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Search Nav')
  await createTicket(page.request, key, { title: 'Findable via the nav box' })

  await page.goto(`/projects/${key}`)
  await page.getByLabel('Search').fill('Findable via the nav box')
  await page.getByLabel('Search').press('Enter')

  await expect(page).toHaveURL(/\/search\?q=/)
  await expect(page.getByText('Findable via the nav box')).toBeVisible()
})

test('search with no query prompts instead of erroring', async ({ page }) => {
  await login(page)
  await page.goto('/search')
  await expect(page.getByText('Enter a search term above.')).toBeVisible()
})
