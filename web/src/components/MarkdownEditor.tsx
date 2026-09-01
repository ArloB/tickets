import { useId, useRef, useState, type KeyboardEvent } from 'react'
import { Markdown } from './Markdown'
import { applyMarkdownAction, type MarkdownAction } from './markdownActions'

type Mode = 'write' | 'split' | 'preview'

const toolbarButtons: Array<{ action: MarkdownAction; label: string; title: string }> = [
  { action: 'bold', label: 'B', title: 'Bold (Ctrl+B)' },
  { action: 'italic', label: 'I', title: 'Italic (Ctrl+I)' },
  { action: 'code', label: '</>', title: 'Inline code' },
  { action: 'link', label: 'Link', title: 'Link (Ctrl+K)' },
  { action: 'heading', label: 'H', title: 'Heading' },
  { action: 'bullet', label: 'List', title: 'Bulleted list' },
  { action: 'quote', label: 'Quote', title: 'Blockquote' },
]

const modes: Mode[] = ['write', 'split', 'preview']

const shortcuts: Record<string, MarkdownAction> = { b: 'bold', i: 'italic', k: 'link' }

export function MarkdownEditor({
  label,
  value,
  onChange,
  projectKey = '',
  rows = 16,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  /** Scopes the ticket-only short form (#123) in the preview, so what
   * the preview links matches what the saved body will link. */
  projectKey?: string
  rows?: number
}) {
  const [mode, setMode] = useState<Mode>('write')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const id = useId()

  function runAction(action: MarkdownAction) {
    const el = textareaRef.current
    if (!el) return
    const next = applyMarkdownAction({ value, start: el.selectionStart, end: el.selectionEnd }, action)
    onChange(next.value)
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(next.start, next.end)
    })
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (!e.ctrlKey && !e.metaKey) return
    const action = shortcuts[e.key.toLowerCase()]
    if (!action) return
    e.preventDefault()
    runAction(action)
  }

  const showWrite = mode !== 'preview'
  const showPreview = mode !== 'write'

  return (
    <div className="md-editor" data-mode={mode}>
      <label className="md-editor-label" htmlFor={id}>
        {label}
      </label>

      <div className="md-editor-bar">
        <div className="md-editor-toolbar">
          {toolbarButtons.map((b) => (
            <button
              key={b.action}
              type="button"
              title={b.title}
              aria-label={b.title}
              disabled={mode === 'preview'}
              onClick={() => runAction(b.action)}
            >
              {b.label}
            </button>
          ))}
        </div>
        <div className="md-editor-modes">
          {modes.map((m) => (
            <button key={m} type="button" aria-pressed={mode === m} onClick={() => setMode(m)}>
              {m === 'write' ? 'Write' : m === 'split' ? 'Split' : 'Preview'}
            </button>
          ))}
        </div>
      </div>

      <div className="md-editor-panes">
        {showWrite && (
          <textarea
            id={id}
            ref={textareaRef}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={onKeyDown}
            rows={rows}
            spellCheck
            placeholder="Write Markdown… reference other records with ABC-1 or #1."
          />
        )}
        {showPreview && (
          <div className="md-editor-preview" data-testid="markdown-editor-preview">
            <Markdown projectKey={projectKey}>
              {value.trim() === '' ? '*Nothing to preview.*' : value}
            </Markdown>
          </div>
        )}
      </div>

      <p className="md-editor-hint">
        Markdown supported. Ctrl+B bold, Ctrl+I italic, Ctrl+K link.
      </p>
    </div>
  )
}
