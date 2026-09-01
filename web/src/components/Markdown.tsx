import type { ComponentPropsWithoutRef } from 'react'
import { Link } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import remarkGfm from 'remark-gfm'
import { useResolvedRefs } from '../hooks/useResolvedRefs'
import { refLinkClass, remarkRefLinks } from './refLinks'

// defaultSchema strips script tags, event handlers, and any href/src
// scheme other than http/https/mailto/tel — the allow-list product
// spec §10 requires and internal/domain.ValidateLinkURL enforces
// server-side for external links. The one widening: the single
// className remarkRefLinks stamps on a reference link, so those can
// be styled apart from an author's own inline links. The value is
// pinned, not the attribute — a body cannot smuggle an arbitrary
// class through by writing raw HTML.
// defaultSchema already carries a value-pinned className entry on <a>
// (for GFM footnote backrefs), and hast-util-sanitize honors only the
// first entry it finds for a property — appending a second one
// silently drops the class instead of allowing it. The allowed value
// is added to the existing entry for that reason.
const anchorAttributes = (defaultSchema.attributes?.a ?? []).map((attr) =>
  Array.isArray(attr) && attr[0] === 'className' ? [...attr, refLinkClass] : attr,
)

const schema = {
  ...defaultSchema,
  attributes: { ...defaultSchema.attributes, a: anchorAttributes },
}

/** The single component every Markdown body render goes through
 * (ticket/feature/decision description, content-item body, comment
 * body) — product spec §10 requires sanitization regardless of CSP,
 * and §15 names this as an explicit unit-test target (see
 * Markdown.test.tsx's XSS payload table).
 *
 * projectKey scopes the ticket-only short form (#123) the same way
 * domain.ScanReferences' scopeProjectKey does; omit it and only fully
 * qualified references are recognized. */
export function Markdown({ children, projectKey = '' }: { children: string; projectKey?: string }) {
  const resolved = useResolvedRefs(children, projectKey)

  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, [remarkRefLinks, { projectKey, resolved }]]}
      rehypePlugins={[[rehypeSanitize, schema]]}
      components={{ a: MarkdownLink }}
    >
      {children}
    </ReactMarkdown>
  )
}

// An in-app href must navigate through the router, not reload the
// whole SPA — every reference link remarkRefLinks emits is one of
// these (detailRoute always returns a root-relative path). Anything
// else keeps the plain <a> it has always had.
function MarkdownLink({ href, children, ...rest }: ComponentPropsWithoutRef<'a'>) {
  if (href !== undefined && href.startsWith('/')) {
    return (
      <Link to={href} {...rest}>
        {children}
      </Link>
    )
  }
  return (
    <a href={href} {...rest}>
      {children}
    </a>
  )
}
