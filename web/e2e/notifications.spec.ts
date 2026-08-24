import { test, expect, type APIRequestContext } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiPost, ifMatchHeader, apiRequest } from './helpers.js'

/** Creates an agent and returns a bearer token for it, authenticated
 * as whichever session ctx already carries (the admin session, in
 * every test below) — mirrors admin.spec.ts's UI-driven agent/token
 * creation, but over the API directly since these tests need the raw
 * token to act as a second identity, not to exercise the admin UI. */
async function createAgentToken(ctx: APIRequestContext, name: string): Promise<string> {
  await apiPost(ctx, '/agents', { name })
  const created = await apiPost<{ token: string }>(ctx, `/agents/${name}/tokens`, { description: 'e2e notifications' })
  return created.token
}

test('creating a ticket auto-subscribes; the Subscribe button toggles it off and on', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Notifications')
  const ticket = await createTicket(page.request, key, { title: 'Watch me' })

  await page.goto(`/tickets/${ticket.ref}`)
  const toggle = page.getByRole('button', { name: /^(Subscribe|Unsubscribe)$/ })
  await expect(toggle).toHaveText('Unsubscribe')

  await toggle.click()
  await expect(toggle).toHaveText('Subscribe')

  await toggle.click()
  await expect(toggle).toHaveText('Unsubscribe')
})

test('a second actor commenting notifies the subscribed creator, and the inbox can mark it read', async ({ page, request }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Notify')
  const agentName = randomKey('agent').toLowerCase()
  const token = await createAgentToken(page.request, agentName)

  const ticket = await createTicket(page.request, key, { title: 'Needs a reply' })

  // The agent comments through the `request` fixture — a separate,
  // cookie-free APIRequestContext from `page.request` (which shares
  // the browser session's cookies). The CSRF check applies whenever a
  // session cookie is present regardless of an Authorization header
  // also being sent, so a bearer-token-only request needs a context
  // with no session cookie at all (no CSRF needed for bearer auth,
  // docs/contracts/cli.md).
  const commentResp = await request.post(`/api/v1/tickets/${ticket.ref}/comments`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { body: 'agent reply' },
  })
  expect(commentResp.ok()).toBe(true)

  await page.goto('/notifications')
  await expect(page.getByText('commented')).toBeVisible()
  await expect(page.getByRole('link', { name: ticket.ref })).toBeVisible()
  await expect(page.getByText(`from agent:${agentName}`)).toBeVisible()

  await page.getByRole('button', { name: 'Mark read' }).click()
  await expect(page.getByText('(read)')).toBeVisible()

  await page.getByLabel('Unread only').check()
  await expect(page.getByText('No notifications.')).toBeVisible()
})

test('assigning a ticket to an agent notifies the agent, not the assigner', async ({ page }) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Assign')
  const agentName = randomKey('agent').toLowerCase()
  await createAgentToken(page.request, agentName)
  const ticket = await createTicket(page.request, key, { title: 'Assign this' })

  await apiRequest(page.request, 'POST', `/tickets/${ticket.ref}/assign`, { assignee: `agent:${agentName}` }, ifMatchHeader(ticket.version))

  // The assigning actor (the admin session driving this test) must not
  // see an "assigned" notification about their own action.
  await page.goto('/notifications')
  await expect(page.getByText('assigned')).toHaveCount(0)
})
