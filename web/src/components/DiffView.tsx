import type { DiffLine } from '../api/types'

/** Renders a computed line-level diff (§5.9) as a plain +/-/context
 * listing — the first version-history/diff UI in this app; there is no
 * existing component to build on (Markdown.tsx is read-only rendering,
 * not a diff view). */
export function DiffView({ lines }: { lines: DiffLine[] }) {
  if (lines.length === 0) return <p>No change.</p>

  return (
    <pre>
      {lines.map((line, i) => {
        const prefix = line.op === 'add' ? '+' : line.op === 'remove' ? '-' : ' '
        return (
          <div key={i} data-diff-op={line.op}>
            {prefix} {line.text}
          </div>
        )
      })}
    </pre>
  )
}
