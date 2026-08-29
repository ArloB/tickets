import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Markdown } from './Markdown'
import * as refsApi from '../api/refs'

vi.mock('../api/refs', async (importOriginal) => ({
  ...(await importOriginal<typeof refsApi>()),
  resolveRefs: vi.fn(),
}))

const resolveRefs = vi.mocked(refsApi.resolveRefs)

function renderMarkdown(body: string, projectKey?: string) {
  return render(
    <MemoryRouter>
      <Markdown projectKey={projectKey}>{body}</Markdown>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  resolveRefs.mockReset()
  resolveRefs.mockResolvedValue([])
})

// The XSS payload table product spec §15 calls out explicitly. Each
// case renders a body a malicious ticket description/comment could
// contain and asserts the dangerous construct never reaches the DOM —
// not just that it doesn't execute (jsdom doesn't execute scripts
// anyway), but that the sanitized output contains none of the
// attributes/elements/schemes an XSS payload needs.
describe('Markdown sanitization', () => {
  it('strips a raw <script> tag', () => {
    const { container } = renderMarkdown('before <script>alert(1)</script> after')
    expect(container.querySelector('script')).toBeNull()
  })

  it('strips an onerror handler on a raw <img>', () => {
    const { container } = renderMarkdown('<img src="x" onerror="alert(1)">')
    const img = container.querySelector('img')
    if (img) {
      expect(img.getAttribute('onerror')).toBeNull()
    }
  })

  it('strips a javascript: href on a Markdown link', () => {
    const { container } = renderMarkdown('[click me](javascript:alert(1))')
    const href = container.querySelector('a')?.getAttribute('href') ?? ''
    expect(/^javascript:/i.test(href)).toBe(false)
  })

  it('strips a data: href on a Markdown link', () => {
    const { container } = renderMarkdown('[click me](data:text/html,<script>alert(1)</script>)')
    const href = container.querySelector('a')?.getAttribute('href') ?? ''
    expect(/^data:/i.test(href)).toBe(false)
  })

  it('strips an onclick handler embedded via raw HTML', () => {
    const { container } = renderMarkdown('<div onclick="alert(1)">hi</div>')
    expect(container.querySelector('[onclick]')).toBeNull()
  })

  it('preserves a safe https link', () => {
    const { container } = renderMarkdown('[safe](https://example.com)')
    const link = screen.getByText('safe')
    expect(link.getAttribute('href')).toBe('https://example.com')
    expect(container.querySelector('a')).not.toBeNull()
  })

  // The widened schema (attributes.a gains a pinned className) and the
  // relative hrefs reference links use both have to survive sanitize —
  // neither was exercised by the table above, and silently losing
  // either would turn every reference link into inert text.
  it('preserves a root-relative href and the pinned reference class', () => {
    renderMarkdown('[x](/tickets/ABC-1)')
    const link = screen.getByText('x')
    expect(link.getAttribute('href')).toBe('/tickets/ABC-1')
  })

  it('renders GFM tables', () => {
    renderMarkdown('| a | b |\n| --- | --- |\n| 1 | 2 |')
    expect(screen.getByText('1')).toBeTruthy()
  })
})

describe('Markdown reference links', () => {
  it('links a reference that resolves and leaves one that does not as text', async () => {
    resolveRefs.mockResolvedValue([
      { ref: 'LNKA-1', exists: true, kind: 'ticket', title: 'Real one' },
      { ref: 'LNKA-99', exists: false },
    ])

    renderMarkdown('See LNKA-1 and LNKA-99.', 'LNKA')

    const link = await screen.findByText('LNKA-1')
    expect(link.tagName).toBe('A')
    expect(link.getAttribute('href')).toBe('/tickets/LNKA-1')
    expect(link.getAttribute('title')).toBe('LNKA-1: Real one')
    expect(link.className).toBe('ref-link')
    await waitFor(() => expect(screen.queryByText('LNKA-99')).toBeNull())
  })

  it('routes each kind letter to its own detail route', async () => {
    resolveRefs.mockResolvedValue([
      { ref: 'LNKB-F1', exists: true, kind: 'feature', title: 'A feature' },
      { ref: 'LNKB-D1', exists: true, kind: 'decision', title: 'A decision' },
      { ref: 'LNKB-P1', exists: true, kind: 'plan', title: 'A plan' },
      { ref: 'LNKB-DOC1', exists: true, kind: 'document', title: 'A document' },
    ])

    renderMarkdown('LNKB-F1 LNKB-D1 LNKB-P1 LNKB-DOC1', 'LNKB')

    expect((await screen.findByText('LNKB-F1')).getAttribute('href')).toBe('/features/LNKB-F1')
    expect(screen.getByText('LNKB-D1').getAttribute('href')).toBe('/decisions/LNKB-D1')
    expect(screen.getByText('LNKB-P1').getAttribute('href')).toBe('/plans/LNKB-P1')
    expect(screen.getByText('LNKB-DOC1').getAttribute('href')).toBe('/documents/LNKB-DOC1')
  })

  it('keeps the "#" of a "#"-prefixed reference in the link text', async () => {
    resolveRefs.mockResolvedValue([{ ref: 'LNKC-1', exists: true, kind: 'ticket', title: 'Hashed' }])

    renderMarkdown('Blocked by #LNKC-1.', 'LNKC')

    expect((await screen.findByText('#LNKC-1')).getAttribute('href')).toBe('/tickets/LNKC-1')
  })

  it('resolves the short form only when a project key scopes it', async () => {
    resolveRefs.mockResolvedValue([{ ref: 'LNKD-7', exists: true, kind: 'ticket', title: 'Short' }])

    const scoped = renderMarkdown('Fixes #7.', 'LNKD')
    expect((await screen.findByText('#7')).getAttribute('href')).toBe('/tickets/LNKD-7')
    scoped.unmount()

    resolveRefs.mockClear()
    renderMarkdown('Fixes #7.')
    await waitFor(() => expect(resolveRefs).not.toHaveBeenCalled())
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('does not linkify a reference inside a code fence or an inline code span', async () => {
    resolveRefs.mockResolvedValue([{ ref: 'LNKE-1', exists: true, kind: 'ticket', title: 'Quoted' }])

    renderMarkdown('Inline `LNKE-1` and fenced:\n\n```\nLNKE-1\n```\n', 'LNKE')

    await waitFor(() => expect(screen.queryByRole('link')).toBeNull())
  })

  it('does not nest a reference link inside an existing Markdown link', async () => {
    resolveRefs.mockResolvedValue([{ ref: 'LNKF-1', exists: true, kind: 'ticket', title: 'Nested' }])

    const { container } = renderMarkdown('[LNKF-1](https://example.com/elsewhere)', 'LNKF')

    await waitFor(() => expect(container.querySelectorAll('a')).toHaveLength(1))
    expect(screen.getByText('LNKF-1').getAttribute('href')).toBe('https://example.com/elsewhere')
  })

  it('does not linkify a reference that is part of a longer identifier', async () => {
    resolveRefs.mockResolvedValue([{ ref: 'LNKG-1', exists: true, kind: 'ticket', title: 'Bounded' }])

    renderMarkdown('_LNKG-1 and LNKG-1x are not references.', 'LNKG')

    await waitFor(() => expect(screen.queryByRole('link')).toBeNull())
  })
})
