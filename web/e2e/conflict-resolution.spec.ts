import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey, apiLogin, apiRequest } from './helpers.js'

// The Phase 4 exit criterion (plan.md): "a human can carry out the
// core workflow entirely in the web UI, including resolving an
// optimistic concurrency conflict without losing edits."
//
// Session A (this test's `page`, a real browser) starts editing a
// ticket's title and description but doesn't submit yet. Session B (a
// separate, independently-authenticated APIRequestContext — a
// concurrent editor A can't see) changes title (the same field A is
// mid-edit on, to a different value: a real conflict) and priority (a
// field A never touches: should auto-apply silently) and saves first.
// A then submits its now-stale draft and must see the conflict
// banner, resolve the title conflict by keeping its own value, and
// resubmit successfully — ending with A's title, A's description, and
// B's priority all present together.
test("conflict resolution keeps session A's draft and merges session B's untouched-field change", async ({
  page,
  request,
}) => {
  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'Conflict Resolution')
  const ticket = await createTicket(page.request, key, {
    type: 'bug',
    title: 'Fix the parser',
    description: 'original description',
    priority: 'high',
    severity: 'high',
  })

  await page.goto(`/tickets/${ticket.ref}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Title').fill("A's title change")
  await page.getByLabel('Description').fill("A's description change")

  // Session B: an independent login, invisible to A's browser session
  // until A's next fetch. Changes title (conflict) and priority
  // (auto-apply candidate) and saves — bumping the ticket to version 2
  // while A is still holding version 1 in its form.
  await apiLogin(request)
  await apiRequest(request, 'PUT', `/tickets/${ticket.ref}`, {
    type: 'bug',
    title: "B's title change",
    description: 'original description',
    priority: 'critical',
    severity: 'high',
  }, { 'If-Match': '"1"' })

  await page.getByRole('button', { name: 'Save' }).click()

  const banner = page.locator('.conflict-banner')
  await expect(banner).toBeVisible()
  await expect(banner).toContainText('now at version 2')
  // Priority was never touched by A, so it must be reported as
  // auto-applied prose, not offered as a fieldset the user has to
  // decide on.
  await expect(banner).toContainText('Priority')
  await expect(banner.getByRole('group', { name: 'Priority' })).toHaveCount(0)

  // Title is a real conflict: both sides changed it differently.
  const titleFieldset = banner.getByRole('group', { name: 'Title' })
  await expect(titleFieldset).toBeVisible()
  // .click(), not .check(): choosing a resolution immediately removes
  // this fieldset from the DOM (useConflictForm drops a resolved field
  // from `conflict.fields`), so .check()'s post-click verification
  // that the radio ended up checked would hang waiting on a node
  // that's already gone.
  await titleFieldset.getByLabel(/Your change/).click()

  await page.getByRole('button', { name: 'Save' }).click()

  // Back in read mode, showing the merged result: A's title, A's
  // description (never touched by B, never conflicted), B's priority
  // (auto-applied since A never touched it).
  await expect(page.getByRole('heading', { level: 1 })).toContainText("A's title change")
  await expect(page.locator('main')).toContainText('critical')
  await expect(page.locator('main')).toContainText("A's description change")
})
