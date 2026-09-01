import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { detailRoute, type ResolvedRef } from '../api/refs'
import { useResolvedRefs } from '../hooks/useResolvedRefs'
import { refLinkClass, scanRefs } from './refLinks'

export function InlineRefText({ text, projectKey = '' }: { text: string; projectKey?: string }) {
  const resolved = useResolvedRefs(text, projectKey)
  const matches = scanRefs(text, projectKey).filter((m) => resolved.get(m.token)?.exists === true)
  if (matches.length === 0) return <>{text}</>

  const parts: ReactNode[] = []
  let cursor = 0
  for (const m of matches) {
    if (m.index > cursor) parts.push(text.slice(cursor, m.index))
    const r = resolved.get(m.token) as ResolvedRef
    parts.push(
      <Link key={m.index} to={detailRoute(m.token)} className={refLinkClass} title={r.title ? `${m.token}: ${r.title}` : m.token}>
        {m.text}
      </Link>,
    )
    cursor = m.index + m.text.length
  }
  if (cursor < text.length) parts.push(text.slice(cursor))
  return <>{parts}</>
}
