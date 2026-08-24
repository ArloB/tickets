import { test, expect, type APIRequestContext } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiPost } from './helpers.js'

/** Mirrors notifications.spec.ts's helper: an agent bearer token,
 * used as a genuinely distinct second actor so a comment on it
 * actually produces a notification (ADR 0019 never notifies an actor
 * about their own action). */
async function createAgentToken(ctx: APIRequestContext, name: string): Promise<string> {
  await apiPost(ctx, '/agents', { name })
  const created = await apiPost<{ token: string }>(ctx, `/agents/${name}/tokens`, { description: 'e2e sse' })
  return created.token
}

// Phase 5's exit criterion, taken literally: "two browsers observe
// changes and notifications without manual full-page refresh." Both
// contexts here are real, independently authenticated sessions — the
// second is never told to reload; a passing assertion only holds if
// the SSE change hint actually reached it and the page refetched on
// its own (ADR 0020).
test('a ticket status change made in one browser appears live in a second browser', async ({
  page,
  browser,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Live Updates')
  const ticket = await createTicket(page.request, key, { title: 'Watch me move' })

  const second = await browser.newContext()
  const secondPage = await second.newPage()
  await login(secondPage)
  await secondPage.goto(`/tickets/${ticket.ref}`)
  await expect(secondPage.getByText('task · backlog · medium')).toBeVisible()

  // The first browser changes the ticket's status through its own
  // board — an ordinary user action, not a raw API call — while the
  // second browser's detail page is already open and idle.
  await page.goto(`/projects/${key}/board`)
  const card = page.locator('li.board-card', { hasText: ticket.ref })
  await card.getByLabel('Move to').selectOption('ready')

  // No reload() call here: the assertion only passes if the second
  // browser's own SSE connection delivered the hint and its detail
  // page refetched on its own.
  await expect(secondPage.getByText('task · ready · medium')).toBeVisible({ timeout: 10_000 })

  await second.close()
})

test('a comment posted by one actor triggers a live notification for another, already-open browser', async ({
  page,
  browser,
  request,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Live Notify')
  const agentName = randomKey('agent').toLowerCase()
  const token = await createAgentToken(page.request, agentName)
  const ticket = await createTicket(page.request, key, { title: 'Reply to me' })

  // The admin session (the ticket's creator, auto-subscribed) opens
  // its notification inbox and leaves it sitting there. The suite
  // runs this alongside other specs sharing the same admin actor, so
  // the inbox is never asserted empty — only that *this* ticket's
  // notification (identified by its unique ref, since ticket.ref is
  // freshly allocated for this test) is absent before and present
  // after, which parallel noise from unrelated tests can't produce.
  const second = await browser.newContext()
  const secondPage = await second.newPage()
  await login(secondPage)
  await secondPage.goto('/notifications')
  const notificationRow = secondPage.getByRole('link', { name: ticket.ref })
  await expect(notificationRow).toHaveCount(0)

  // A different actor (the agent) comments, out of band, through the
  // separate cookie-free `request` fixture — a session-cookie-bearing
  // context CSRF-rejects even a bearer-token request (notifications.
  // spec.ts's own finding), so this must not reuse page.request.
  const commentResp = await request.post(`/api/v1/tickets/${ticket.ref}/comments`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { body: 'Live-updated reply' },
  })
  expect(commentResp.ok()).toBe(true)

  // No reload() on secondPage: this only passes if its live
  // notifications_changed hint arrived and the inbox refetched itself.
  await expect(notificationRow).toBeVisible({ timeout: 10_000 })
  await expect(secondPage.getByText(`from agent:${agentName}`)).toBeVisible()

  await second.close()
})
