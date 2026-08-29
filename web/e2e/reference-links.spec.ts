import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiPost } from './helpers.js'

// ADR 0025: references written into prose render as links to the
// record they name, but only once the server confirms it exists.
// Everything here is the real pipeline — a real body stored through
// the API, the real GET /refs/resolve, the real remark plugin — which
// is what the jsdom component tests (Markdown.test.tsx, where
// resolveRefs is mocked) deliberately cannot cover.
test('a ticket description links its resolvable references and leaves the rest as text', async ({
  page,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Reference links')

  const target = await createTicket(page.request, key, { title: 'The linked ticket' })
  const plan = await apiPost<{ ref: string }>(page.request, `/projects/${key}/plans`, {
    title: 'The linked plan',
    representation: 'markdown',
    body: 'nothing to see here',
  })
  const source = await createTicket(page.request, key, {
    title: 'The referring ticket',
    description: `Blocked by ${target.ref}, planned in ${plan.ref}, and ${key}-999 does not exist.`,
  })

  await page.goto(`/tickets/${source.ref}`)

  const ticketLink = page.getByRole('link', { name: target.ref, exact: true })
  await expect(ticketLink).toHaveAttribute('href', `/tickets/${target.ref}`)
  await expect(ticketLink).toHaveAttribute('title', `${target.ref}: The linked ticket`)
  await expect(page.getByRole('link', { name: plan.ref, exact: true })).toHaveAttribute(
    'href',
    `/plans/${plan.ref}`,
  )
  await expect(page.getByRole('link', { name: `${key}-999`, exact: true })).toHaveCount(0)

  await ticketLink.click()
  await expect(page).toHaveURL(new RegExp(`/tickets/${target.ref}$`))
  await expect(page.getByRole('heading', { name: 'The linked ticket' })).toBeVisible()
})

// The short form is the one recognition rule that depends on knowing
// which project the body belongs to, and a comment is where §5.2's
// own example puts it.
test('a comment linkifies the project-scoped short form', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Short form')

  const target = await createTicket(page.request, key, { title: 'Short form target' })
  const seq = target.ref.slice(key.length + 1)
  const host = await createTicket(page.request, key, { title: 'Comment host' })
  await apiPost(page.request, `/tickets/${host.ref}/comments`, { body: `Duplicate of #${seq}.` })

  await page.goto(`/tickets/${host.ref}`)

  const link = page.getByRole('link', { name: `#${seq}`, exact: true })
  await expect(link).toHaveAttribute('href', `/tickets/${target.ref}`)
  await link.click()
  await expect(page).toHaveURL(new RegExp(`/tickets/${target.ref}$`))
})
