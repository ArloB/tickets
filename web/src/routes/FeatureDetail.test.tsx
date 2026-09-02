import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import FeatureDetail, { FeatureOverview } from './FeatureDetail'
import { ApiError } from '../api/client'
import * as featuresApi from '../api/features'
import * as linksApi from '../api/links'
import * as associationsApi from '../api/associations'
import * as backlinksApi from '../api/backlinks'
import * as attachmentsApi from '../api/attachments'
import * as commentsApi from '../api/comments'
import { useAuth } from '../auth/AuthContext'
import type { FeatureDetail as FeatureDetailDto } from '../api/types'

vi.mock('../api/features', async () => {
  const actual = await vi.importActual<typeof import('../api/features')>('../api/features')
  return { ...actual, getFeature: vi.fn(), deleteFeature: vi.fn(), restoreFeature: vi.fn() }
})
vi.mock('../api/links', () => ({ listLinks: vi.fn() }))
vi.mock('../api/associations', () => ({ listAssociations: vi.fn() }))
vi.mock('../api/backlinks', () => ({ listBacklinks: vi.fn() }))
vi.mock('../api/attachments', () => ({ listAttachments: vi.fn() }))
vi.mock('../api/comments', () => ({ listComments: vi.fn() }))
vi.mock('../auth/AuthContext', () => ({ useAuth: vi.fn() }))

const getFeature = vi.mocked(featuresApi.getFeature)
const deleteFeature = vi.mocked(featuresApi.deleteFeature)
const restoreFeature = vi.mocked(featuresApi.restoreFeature)
const listLinks = vi.mocked(linksApi.listLinks)
const listAssociations = vi.mocked(associationsApi.listAssociations)
const listBacklinks = vi.mocked(backlinksApi.listBacklinks)
const listAttachments = vi.mocked(attachmentsApi.listAttachments)
const listComments = vi.mocked(commentsApi.listComments)
const mockUseAuth = vi.mocked(useAuth)

const hasDependents = new ApiError(
  {
    code: 'has_dependents',
    message: 'feature ABC-F2 has 2 non-deleted ticket(s); retry with cascade to delete them together',
    field: null,
    correlation_id: 'c1',
    current_version: null,
  },
  409,
)

function feature(overrides: Partial<FeatureDetailDto> = {}): FeatureDetailDto {
  return {
    ref: 'ABC-F2',
    project: 'ABC',
    title: 'Payments',
    description: 'Body',
    status: 'backlog',
    priority: 'medium',
    version: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderFeatureDetail(ref = 'ABC-F2') {
  return render(
    <MemoryRouter initialEntries={[`/features/${ref}`]}>
      <Routes>
        <Route path="/features/:ref" element={<FeatureDetail />}>
          <Route index element={<FeatureOverview />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  listLinks.mockResolvedValue({ links: [] })
  listAssociations.mockResolvedValue({ associated: [] })
  listBacklinks.mockResolvedValue({ backlinks: [] })
  listAttachments.mockResolvedValue({ attachments: [] })
  listComments.mockResolvedValue({ comments: [] })
  mockUseAuth.mockReturnValue({
    me: { permission: 'editor', is_admin: false, actor: 'human:admin' },
    ready: true,
    bootstrapError: null,
    login: vi.fn(),
    logout: vi.fn(),
  })
})

describe('FeatureDetail — delete/restore (ADR 0001, ADR 0013)', () => {
  it('never offers Delete on the General feature', async () => {
    getFeature.mockResolvedValueOnce(feature({ ref: 'ABC-F1', title: 'General' }))

    renderFeatureDetail('ABC-F1')

    expect(await screen.findByRole('heading', { name: /General/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  })

  it('surfaces has_dependents and offers cascade delete instead of silently retrying', async () => {
    const user = userEvent.setup()
    getFeature.mockResolvedValueOnce(feature())
    deleteFeature.mockRejectedValueOnce(hasDependents).mockResolvedValueOnce({ version: 2 })
    getFeature.mockRejectedValueOnce(
      new ApiError({ code: 'not_found', message: 'feature not found', field: null, correlation_id: 'c1', current_version: null }, 404),
    )
    getFeature.mockResolvedValueOnce(feature({ version: 2, deleted_at: '2026-02-01T00:00:00Z' }))

    renderFeatureDetail()
    await user.click(await screen.findByRole('button', { name: 'Delete' }))

    expect(deleteFeature).toHaveBeenNthCalledWith(1, 'ABC-F2', 1, false)
    expect(await screen.findByText(/has 2 non-deleted ticket/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Delete feature and its tickets' }))
    expect(deleteFeature).toHaveBeenNthCalledWith(2, 'ABC-F2', 1, true)

    expect(await screen.findByText(/This feature was deleted/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('restores a deleted feature reached via a stale link', async () => {
    const user = userEvent.setup()
    getFeature
      .mockRejectedValueOnce(
        new ApiError({ code: 'not_found', message: 'feature not found', field: null, correlation_id: 'c1', current_version: null }, 404),
      )
      .mockResolvedValueOnce(feature({ version: 2, deleted_at: '2026-02-01T00:00:00Z' }))
      .mockResolvedValueOnce(feature({ version: 3 }))
    restoreFeature.mockResolvedValueOnce(feature({ version: 3 }))

    renderFeatureDetail()

    expect(await screen.findByText(/This feature was deleted/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Restore' }))

    expect(restoreFeature).toHaveBeenCalledWith('ABC-F2', 2)
    expect(await screen.findByRole('heading', { name: /Payments/ })).toBeInTheDocument()
  })
})
