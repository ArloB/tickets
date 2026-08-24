import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { MarkdownEditor } from './MarkdownEditor'

describe('MarkdownEditor', () => {
  it('starts on the edit tab and calls onChange as the textarea is typed in', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<MarkdownEditor label="Body" value="" onChange={onChange} />)

    const textarea = screen.getByLabelText('Body')
    await user.type(textarea, 'hi')

    expect(onChange).toHaveBeenCalledWith('h')
    expect(onChange).toHaveBeenCalledWith('i')
    expect(screen.queryByTestId('markdown-editor-preview')).toBeNull()
  })

  it('switches to a rendered Markdown preview on the preview tab', async () => {
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
})
