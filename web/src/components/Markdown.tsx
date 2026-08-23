import ReactMarkdown from 'react-markdown'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import remarkGfm from 'remark-gfm'

// The single component every Markdown body render goes through
// (ticket/feature/decision description, comment body) — product spec
// §10 requires sanitization regardless of CSP, and §15 names this as
// an explicit unit-test target (see Markdown.test.tsx's XSS payload
// table: <script>, onerror=, javascript: hrefs). rehype-sanitize's
// defaultSchema strips script tags, event handlers, and any href/src
// scheme other than http/https/mailto/tel — the same allow-list
// internal/domain.ValidateLinkURL enforces server-side for external
// links, applied here to inline Markdown links too.
export function Markdown({ children }: { children: string }) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[[rehypeSanitize, defaultSchema]]}>
      {children}
    </ReactMarkdown>
  )
}
