import { useId, useState } from 'react'
import { Markdown } from './Markdown'

// The first split edit/preview component in the app — every other
// editable Markdown field today is a bare <textarea> with no preview
// (product spec §5.9's plan/document editing is what first calls for
// one). Deliberately minimal: a two-tab toggle between the raw
// <textarea> and the same <Markdown> renderer every read-only view
// already uses, so the preview can never drift from what a reader will
// actually see.
export function MarkdownEditor({
  label,
  value,
  onChange,
  rows = 10,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  rows?: number
}) {
  const [tab, setTab] = useState<'edit' | 'preview'>('edit')
  const id = useId()

  return (
    <div>
      <label htmlFor={id}>{label}</label>
      <div role="tablist" aria-label={`${label} view`}>
        <button type="button" aria-pressed={tab === 'edit'} onClick={() => setTab('edit')}>
          Edit
        </button>
        <button type="button" aria-pressed={tab === 'preview'} onClick={() => setTab('preview')}>
          Preview
        </button>
      </div>
      {tab === 'edit' ? (
        <textarea id={id} value={value} onChange={(e) => onChange(e.target.value)} rows={rows} />
      ) : (
        <div data-testid="markdown-editor-preview">
          <Markdown>{value.trim() === '' ? '*Nothing to preview.*' : value}</Markdown>
        </div>
      )}
    </div>
  )
}
