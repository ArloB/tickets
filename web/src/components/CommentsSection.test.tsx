import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { CommentsSection } from './CommentsSection'
import * as commentsApi from '../api/comments'
import type { CommentDetail } from '../api/types'

vi.mock('../api/comments', async () => {
  const actual = await vi.importActual<typeof import('../api/comments')>('../api/comments')
  return { ...actual, getCommentHistory: vi.fn() }
})

const getCommentHistory = vi.mocked(commentsApi.getCommentHistory)

function comment(overrides: Partial<CommentDetail> = {}): CommentDetail {
  return {
    id: 1,
    author: 'human:admin',
    body: 'Current text',
    version: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('CommentsSection — edit history', () => {
  it('offers no history link for a never-edited comment', () => {
    render(
      <CommentsSection entityRef="ABC-1" comments={[comment()]} onChange={vi.fn()} canEdit={false} />,
    )

    expect(screen.queryByRole('button', { name: 'History' })).not.toBeInTheDocument()
    expect(screen.queryByText('edited')).not.toBeInTheDocument()
  })

  it('shows prior versions on demand for an edited comment, fetched lazily', async () => {
    const user = userEvent.setup()
    getCommentHistory.mockResolvedValueOnce({
      versions: [
        { version: 1, body: 'Original text', edited_by: 'human:admin', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    render(
      <CommentsSection
        entityRef="ABC-1"
        comments={[comment({ version: 2 })]}
        onChange={vi.fn()}
        canEdit={false}
      />,
    )

    expect(getCommentHistory).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'History' }))

    expect(getCommentHistory).toHaveBeenCalledWith(1)
    expect(await screen.findByText('Original text')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Hide history' }))
    expect(screen.queryByText('Original text')).not.toBeInTheDocument()
  })
})
