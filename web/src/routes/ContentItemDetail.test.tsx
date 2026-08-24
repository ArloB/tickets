import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ContentItemDetail from './ContentItemDetail'
import * as contentItemsApi from '../api/content-items'
import * as linksApi from '../api/links'
import * as associationsApi from '../api/associations'
import * as backlinksApi from '../api/backlinks'
import { useAuth } from '../auth/AuthContext'
import type {
  ContentItemDetail as ContentItemDetailDto,
  ContentItemDiff,
  ContentItemVersion,
} from '../api/types'

vi.mock('../api/content-items', async () => {
  const actual = await vi.importActual<typeof import('../api/content-items')>('../api/content-items')
  return {
    ...actual,
    getContentItem: vi.fn(),
    listContentItemVersions: vi.fn(),
    getContentItemDiff: vi.fn(),
    updateContentItem: vi.fn(),
  }
})
vi.mock('../api/links', () => ({ listLinks: vi.fn() }))
vi.mock('../api/associations', () => ({ listAssociations: vi.fn() }))
vi.mock('../api/backlinks', () => ({ listBacklinks: vi.fn() }))
vi.mock('../auth/AuthContext', () => ({ useAuth: vi.fn() }))

const getContentItem = vi.mocked(contentItemsApi.getContentItem)
const listContentItemVersions = vi.mocked(contentItemsApi.listContentItemVersions)
const getContentItemDiff = vi.mocked(contentItemsApi.getContentItemDiff)
const listLinks = vi.mocked(linksApi.listLinks)
const listAssociations = vi.mocked(associationsApi.listAssociations)
const listBacklinks = vi.mocked(backlinksApi.listBacklinks)
const mockUseAuth = vi.mocked(useAuth)

function item(overrides: Partial<ContentItemDetailDto> = {}): ContentItemDetailDto {
  return {
    ref: 'ABC-P1',
    project: 'ABC',
    kind: 'plan',
    title: 'v2 title',
    representation: 'markdown',
    body: 'v2 body',
    version: 2,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...overrides,
  }
}

function renderContentItemDetail() {
  return render(
    <MemoryRouter initialEntries={['/plans/ABC-P1']}>
      <Routes>
        <Route path="/plans/:ref" element={<ContentItemDetail urlKind="plans" />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockUseAuth.mockReturnValue({
    me: { permission: 'editor', is_admin: false },
    ready: true,
    bootstrapError: null,
    login: vi.fn(),
    logout: vi.fn(),
  })
  listLinks.mockResolvedValue({ links: [] })
  listAssociations.mockResolvedValue({ associated: [] })
  listBacklinks.mockResolvedValue({ backlinks: [] })
})

describe('ContentItemDetail version history diff', () => {
  it('renders a diff block for both title and body', async () => {
    const user = userEvent.setup()
    getContentItem.mockResolvedValue(item())
    const archivedVersion: ContentItemVersion = {
      version: 1,
      representation: 'markdown',
      title: 'v1 title',
      body: 'v1 body',
      edited_by: 'human:alice',
      created_at: '2026-01-01T00:00:00Z',
    }
    listContentItemVersions.mockResolvedValue({ versions: [archivedVersion] })
    const diff: ContentItemDiff = {
      from_version: 1,
      to_version: 2,
      title: [
        { op: 'remove', text: 'v1 title' },
        { op: 'add', text: 'v2 title' },
      ],
      body: [
        { op: 'remove', text: 'v1 body' },
        { op: 'add', text: 'v2 body' },
      ],
    }
    getContentItemDiff.mockResolvedValue(diff)

    renderContentItemDetail()

    await waitFor(() => expect(screen.getByText('v1 title')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'Show diff' }))

    const diffSection = await waitFor(() => screen.getByTestId('version-diff'))
    for (const [label, oldText, newText] of [
      ['Title', 'v1 title', 'v2 title'],
      ['Body', 'v1 body', 'v2 body'],
    ] as const) {
      expect(within(diffSection).getByRole('heading', { name: label })).toBeInTheDocument()
      expect(within(diffSection).getByText(new RegExp(oldText))).toBeInTheDocument()
      expect(within(diffSection).getByText(new RegExp(newText))).toBeInTheDocument()
    }
  })

  it('shows "no prior versions" when the item has never been edited', async () => {
    getContentItem.mockResolvedValue(item({ version: 1 }))
    listContentItemVersions.mockResolvedValue({ versions: [] })

    renderContentItemDetail()

    await waitFor(() =>
      expect(screen.getByText('No prior versions — this is the only version.')).toBeInTheDocument(),
    )
  })
})
