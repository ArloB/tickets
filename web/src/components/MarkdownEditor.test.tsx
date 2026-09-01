import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { MarkdownEditor } from './MarkdownEditor'

describe('MarkdownEditor', () => {
  it('starts in write mode and calls onChange as the textarea is typed in', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<MarkdownEditor label="Body" value="" onChange={onChange} />)

    const textarea = screen.getByLabelText('Body')
    await user.type(textarea, 'hi')

    expect(onChange).toHaveBeenCalledWith('h')
    expect(onChange).toHaveBeenCalledWith('i')
    expect(screen.queryByTestId('markdown-editor-preview')).toBeNull()
  })

  it('shows both panes in split mode', async () => {
    const user = userEvent.setup()
    render(<MarkdownEditor label="Body" value="**bold**" onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: 'Split' }))

    expect(screen.getByLabelText('Body')).toBeInTheDocument()
    expect(screen.getByTestId('markdown-editor-preview').querySelector('strong')).not.toBeNull()
  })

  it('replaces the textarea with the rendered preview in preview mode', async () => {
    const user = userEvent.setup()
    render(<MarkdownEditor label="Body" value="**bold**" onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: 'Preview' }))

    const preview = screen.getByTestId('markdown-editor-preview')
    expect(preview.querySelector('strong')).not.toBeNull()
    expect(screen.queryByLabelText('Body')).toBeNull()
  })

  it('shows a placeholder in preview when the body is empty', async () => {
    const user = userEvent.setup()
    render(<MarkdownEditor label="Body" value="" onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: 'Preview' }))

    expect(screen.getByTestId('markdown-editor-preview').textContent).toContain('Nothing to preview')
  })

  it('wraps the current selection when a toolbar button is used', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<MarkdownEditor label="Body" value="hello world" onChange={onChange} />)

    const textarea = screen.getByLabelText('Body') as HTMLTextAreaElement
    textarea.setSelectionRange(0, 5)
    await user.click(screen.getByRole('button', { name: 'Bold (Ctrl+B)' }))

    expect(onChange).toHaveBeenCalledWith('**hello** world')
  })

  it('disables the toolbar in preview mode', async () => {
    const user = userEvent.setup()
    render(<MarkdownEditor label="Body" value="x" onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: 'Preview' }))

    expect(screen.getByRole('button', { name: 'Bold (Ctrl+B)' })).toBeDisabled()
  })
})
