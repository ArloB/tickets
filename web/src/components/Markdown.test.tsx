import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown } from './Markdown'

// The XSS payload table product spec §15 calls out explicitly. Each
// case renders a body a malicious ticket description/comment could
// contain and asserts the dangerous construct never reaches the DOM —
// not just that it doesn't execute (jsdom doesn't execute scripts
// anyway), but that the sanitized output contains none of the
// attributes/elements/schemes an XSS payload needs.
describe('Markdown sanitization', () => {
  it('strips a raw <script> tag', () => {
    const { container } = render(<Markdown>{'before <script>alert(1)</script> after'}</Markdown>)
    expect(container.querySelector('script')).toBeNull()
  })

  it('strips an onerror handler on a raw <img>', () => {
    const { container } = render(
      <Markdown>{'<img src="x" onerror="alert(1)">'}</Markdown>,
    )
    const img = container.querySelector('img')
    if (img) {
      expect(img.getAttribute('onerror')).toBeNull()
    }
  })

  it('strips a javascript: href on a Markdown link', () => {
    const { container } = render(<Markdown>{'[click me](javascript:alert(1))'}</Markdown>)
    const href = container.querySelector('a')?.getAttribute('href') ?? ''
    expect(/^javascript:/i.test(href)).toBe(false)
  })

  it('strips a data: href on a Markdown link', () => {
    const { container } = render(
      <Markdown>{'[click me](data:text/html,<script>alert(1)</script>)'}</Markdown>,
    )
    const href = container.querySelector('a')?.getAttribute('href') ?? ''
    expect(/^data:/i.test(href)).toBe(false)
  })

  it('strips an onclick handler embedded via raw HTML', () => {
    const { container } = render(<Markdown>{'<div onclick="alert(1)">hi</div>'}</Markdown>)
    expect(container.querySelector('[onclick]')).toBeNull()
  })

  it('preserves a safe https link', () => {
    const { container, getByText } = render(<Markdown>{'[safe](https://example.com)'}</Markdown>)
    const link = getByText('safe')
    expect(link.getAttribute('href')).toBe('https://example.com')
    expect(container.querySelector('a')).not.toBeNull()
  })

  it('renders GFM tables', () => {
    const { getByText } = render(
      <Markdown>{'| a | b |\n| --- | --- |\n| 1 | 2 |'}</Markdown>,
    )
    expect(getByText('1')).toBeTruthy()
  })
})
