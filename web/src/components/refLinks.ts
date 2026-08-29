import type { Link, Root, RootContent, Text } from 'mdast'
import type { ResolvedRef } from '../api/refs'
import { detailRoute } from '../api/refs'

/** The class every reference link carries, so a reader can tell a
 * cross-reference from an author's own inline link. Pinned by value
 * in Markdown.tsx's sanitize schema — a body cannot smuggle an
 * arbitrary class through by writing raw HTML. */
export const refLinkClass = 'ref-link'

/** One reference found in prose: the exact source text matched
 * (`#ABC-1`, `ABC-1`, or the short form `#1`) and the canonical token
 * it names. */
export interface RefMatch {
  index: number
  text: string
  token: string
}

// The JS half of docs/contracts/references.md's "Recognition in text"
// grammar, mirroring internal/domain/scan.go's scanPattern and
// shortFormPattern as a single alternation: the five-kind bare/
// '#'-prefixed form first, the project-scoped ticket-only short form
// second. Order matters at a given position — "#ABC-1" must match the
// first branch, and only "#1" (no letters after the '#') can reach the
// second. DOC precedes F/D for the same longest-first reason
// reference.go gives.
const refPattern = /#?([A-Z][A-Z0-9]{1,9})-(DOC|F|D|P)?([1-9][0-9]*)|#([1-9][0-9]*)/g

function isWordChar(c: string): boolean {
  return /[A-Za-z0-9_]/.test(c)
}

// scan.go's isBoundaryOK: a match that is really the tail or head of a
// longer identifier ("XABC-123", "ABC-123x") is not a reference. A
// match starting with '#' is exempt from the leading check, as there.
function boundaryOK(text: string, start: number, end: number): boolean {
  if (start > 0 && text[start] !== '#' && isWordChar(text[start - 1])) return false
  if (end < text.length && isWordChar(text[end])) return false
  return true
}

/** Finds every reference token in one run of plain text. Code fences
 * and inline code spans need no stripping here (unlike scan.go, which
 * works on the raw Markdown source): this runs over mdast text nodes,
 * where remark has already separated `code` and `inlineCode` into
 * their own node types. scopeProjectKey resolves the short form; pass
 * '' to disable it. */
export function scanRefs(text: string, scopeProjectKey: string): RefMatch[] {
  const out: RefMatch[] = []
  refPattern.lastIndex = 0
  for (let m = refPattern.exec(text); m !== null; m = refPattern.exec(text)) {
    const start = m.index
    const end = start + m[0].length
    if (!boundaryOK(text, start, end)) continue
    let token: string
    if (m[1] !== undefined) {
      token = `${m[1]}-${m[2] ?? ''}${m[3]}`
    } else {
      if (scopeProjectKey === '') continue
      token = `${scopeProjectKey}-${m[4]}`
    }
    out.push({ index: start, text: m[0], token })
  }
  return out
}

function refLinkNode(match: RefMatch, resolved: ResolvedRef): Link {
  const label = resolved.title === undefined || resolved.title === '' ? match.token : `${match.token}: ${resolved.title}`
  return {
    type: 'link',
    url: detailRoute(match.token),
    title: label,
    children: [{ type: 'text', value: match.text }],
    data: { hProperties: { className: [refLinkClass] } },
  }
}

function splitTextNode(node: Text, scopeProjectKey: string, resolved: Map<string, ResolvedRef>): RootContent[] | null {
  const matches = scanRefs(node.value, scopeProjectKey).filter((m) => resolved.get(m.token)?.exists === true)
  if (matches.length === 0) return null

  const out: RootContent[] = []
  let cursor = 0
  for (const m of matches) {
    if (m.index > cursor) out.push({ type: 'text', value: node.value.slice(cursor, m.index) })
    out.push(refLinkNode(m, resolved.get(m.token) as ResolvedRef))
    cursor = m.index + m.text.length
  }
  if (cursor < node.value.length) out.push({ type: 'text', value: node.value.slice(cursor) })
  return out
}

interface ParentNode {
  type: string
  children?: RootContent[]
}

/** remarkRefLinks rewrites every resolvable reference in prose into a
 * link to that record's detail route (ADR 0025). A remark plugin
 * rather than a pass over the raw Markdown string, so code blocks are
 * excluded structurally and an existing Markdown link's own text is
 * never wrapped into an illegal nested <a>. Only tokens present in
 * `resolved` with exists=true become links; everything else is left
 * as the plain text it already was. */
export function remarkRefLinks(options: { projectKey: string; resolved: Map<string, ResolvedRef> }) {
  return (tree: Root): void => {
    const walk = (node: ParentNode): void => {
      if (node.children === undefined) return
      if (node.type === 'link' || node.type === 'linkReference') return

      const next: RootContent[] = []
      for (const child of node.children) {
        if (child.type === 'text') {
          const split = splitTextNode(child, options.projectKey, options.resolved)
          if (split !== null) {
            next.push(...split)
            continue
          }
        }
        walk(child as ParentNode)
        next.push(child)
      }
      node.children = next
    }
    walk(tree)
  }
}
