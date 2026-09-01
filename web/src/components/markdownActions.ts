export type MarkdownAction = 'bold' | 'italic' | 'code' | 'link' | 'heading' | 'bullet' | 'quote'

export interface EditorSelection {
  value: string
  start: number
  end: number
}

const wrappers: Partial<Record<MarkdownAction, string>> = {
  bold: '**',
  italic: '*',
  code: '`',
}

const linePrefixes: Partial<Record<MarkdownAction, string>> = {
  heading: '## ',
  bullet: '- ',
  quote: '> ',
}

function toggleWrap(sel: EditorSelection, marker: string): EditorSelection {
  const { value, start, end } = sel
  const selected = value.slice(start, end)
  const before = value.slice(0, start)
  const after = value.slice(end)

  if (selected.startsWith(marker) && selected.endsWith(marker) && selected.length >= marker.length * 2) {
    const inner = selected.slice(marker.length, selected.length - marker.length)
    return { value: before + inner + after, start, end: start + inner.length }
  }

  if (before.endsWith(marker) && after.startsWith(marker)) {
    const trimmedBefore = before.slice(0, before.length - marker.length)
    const trimmedAfter = after.slice(marker.length)
    return {
      value: trimmedBefore + selected + trimmedAfter,
      start: start - marker.length,
      end: end - marker.length,
    }
  }

  return {
    value: `${before}${marker}${selected}${marker}${after}`,
    start: start + marker.length,
    end: end + marker.length,
  }
}

function toggleLinePrefix(sel: EditorSelection, prefix: string): EditorSelection {
  const { value, start, end } = sel
  const lineStart = value.lastIndexOf('\n', start - 1) + 1
  const lineEndIndex = value.indexOf('\n', end)
  const lineEnd = lineEndIndex === -1 ? value.length : lineEndIndex

  const block = value.slice(lineStart, lineEnd)
  const lines = block.split('\n')
  const allPrefixed = lines.every((line) => line.startsWith(prefix))
  const next = lines
    .map((line) => (allPrefixed ? line.slice(prefix.length) : prefix + line))
    .join('\n')

  const delta = allPrefixed ? -prefix.length : prefix.length
  return {
    value: value.slice(0, lineStart) + next + value.slice(lineEnd),
    start: Math.max(lineStart, start + delta),
    end: end + delta * lines.length,
  }
}

function insertLink(sel: EditorSelection): EditorSelection {
  const { value, start, end } = sel
  const selected = value.slice(start, end)
  const text = selected === '' ? 'text' : selected
  const inserted = `[${text}](url)`
  const urlStart = start + text.length + 3
  return {
    value: value.slice(0, start) + inserted + value.slice(end),
    start: urlStart,
    end: urlStart + 3,
  }
}

export function applyMarkdownAction(sel: EditorSelection, action: MarkdownAction): EditorSelection {
  if (action === 'link') return insertLink(sel)
  const wrapper = wrappers[action]
  if (wrapper !== undefined) return toggleWrap(sel, wrapper)
  const prefix = linePrefixes[action]
  if (prefix !== undefined) return toggleLinePrefix(sel, prefix)
  return sel
}
