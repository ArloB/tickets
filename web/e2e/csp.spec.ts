import { test, expect } from '@playwright/test'
import { login, createProject, createTicket, randomKey } from './helpers.js'

// plan.md's Milestone 5 scope calls for verifying the real production
// build against internal/httpapi/static.go's CSP header — no
// `unsafe-inline` in either direction (scripts or styles), so this
// specifically needs a real browser enforcing it, not just a header
// presence check. react-markdown/rehype-sanitize was flagged as the
// concrete risk (an inline `style="..."` or `<script>` slipping through
// sanitized Markdown), so this exercises Markdown rendering, not just
// a static page load.
test('the production build serves a strict CSP and never trips a violation, including with rendered Markdown', async ({ page }) => {
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) => {
      ;(window as unknown as { __cspViolations: string[] }).__cspViolations ??= []
      ;(window as unknown as { __cspViolations: string[] }).__cspViolations.push(
        `${e.violatedDirective}: ${e.blockedURI}`,
      )
    })
  })

  const resp = await page.goto('/')
  const csp = resp?.headers()['content-security-policy']
  expect(csp).toBeTruthy()
  expect(csp).toContain("default-src 'self'")
  expect(csp).not.toContain('unsafe-inline')
  expect(csp).not.toContain('unsafe-eval')

  await login(page)
  const key = randomKey()
  await createProject(page.request, key, 'CSP Check')
  const ticket = await createTicket(page.request, key, {
    title: 'CSP Markdown Ticket',
    description: '# Heading\n\nSome **bold** text, a [link](https://example.com), and `code`.',
  })
  await page.goto(`/tickets/${ticket.ref}`)
  await expect(page.getByText('Heading', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'link', exact: true })).toHaveAttribute(
    'href',
    'https://example.com',
  )

  for (const route of [
    '/',
    `/projects/${key}`,
    `/projects/${key}/backlog`,
    `/projects/${key}/board`,
    `/admin/agents`,
  ]) {
    await page.goto(route)
  }

  const collected = await page.evaluate(
    () => (window as unknown as { __cspViolations?: string[] }).__cspViolations ?? [],
  )
  expect(collected).toEqual([])
})

// The test above asserts zero violations — which is indistinguishable
// from "the listener silently never fires" unless something also
// proves it *would* catch a real one. This closes that gap: inject an
// inline script the CSP must reject, and confirm both that the
// browser actually blocked it from running (not just reported it) and
// that securitypolicyviolation fired for it.
test('a real inline-script injection is both blocked and reported, proving the CSP is actually enforced', async ({
  page,
}) => {
  await page.goto('/')
  const executed = await page.evaluate(() => {
    return new Promise<boolean>((resolve) => {
      let violated = false
      document.addEventListener('securitypolicyviolation', () => {
        violated = true
      })
      const script = document.createElement('script')
      script.textContent = 'window.__cspTestExecuted = true'
      document.head.appendChild(script)
      // securitypolicyviolation fires synchronously-ish on append for
      // a blocked inline script; a microtask tick is enough to observe it.
      setTimeout(() => resolve(violated), 50)
    })
  })
  expect(executed, 'securitypolicyviolation should have fired for the injected script').toBe(true)

  const ran = await page.evaluate(
    () => (window as unknown as { __cspTestExecuted?: boolean }).__cspTestExecuted,
  )
  expect(ran, 'the inline script must not have actually executed').toBeUndefined()
})
