import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DecisionDetail, { DecisionOverview } from './DecisionDetail'
import * as decisionsApi from '../api/decisions'
import * as linksApi from '../api/links'
import * as associationsApi from '../api/associations'
import * as backlinksApi from '../api/backlinks'
import * as attachmentsApi from '../api/attachments'
import * as commentsApi from '../api/comments'
import { useAuth } from '../auth/AuthContext'
import type { DecisionDetail as DecisionDetailDto, DecisionDiff, DecisionVersion } from '../api/types'

vi.mock('../api/decisions', async () => {
  const actual = await vi.importActual<typeof import('../api/decisions')>('../api/decisions')
  return {
    ...actual,
    getDecision: vi.fn(),
    listDecisionVersions: vi.fn(),
    getDecisionDiff: vi.fn(),
  }
})
vi.mock('../api/links', () => ({ listLinks: vi.fn() }))
vi.mock('../api/associations', () => ({ listAssociations: vi.fn() }))
vi.mock('../api/backlinks', () => ({ listBacklinks: vi.fn() }))
vi.mock('../api/attachments', () => ({ listAttachments: vi.fn() }))
vi.mock('../api/comments', () => ({ listComments: vi.fn() }))
vi.mock('../auth/AuthContext', () => ({ useAuth: vi.fn() }))

const getDecision = vi.mocked(decisionsApi.getDecision)
const listDecisionVersions = vi.mocked(decisionsApi.listDecisionVersions)
const getDecisionDiff = vi.mocked(decisionsApi.getDecisionDiff)
const listLinks = vi.mocked(linksApi.listLinks)
const listAssociations = vi.mocked(associationsApi.listAssociations)
const listBacklinks = vi.mocked(backlinksApi.listBacklinks)
const listAttachments = vi.mocked(attachmentsApi.listAttachments)
const listComments = vi.mocked(commentsApi.listComments)
const mockUseAuth = vi.mocked(useAuth)

function decision(overrides: Partial<DecisionDetailDto> = {}): DecisionDetailDto {
  return {
    ref: 'ABC-D1',
    project: 'ABC',
    title: 'v2 title',
    context: 'v2 context',
    decision: 'v2 decision',
    rationale: 'v2 rationale',
    consequences: 'v2 consequences',
    status: 'accepted',
    version: 2,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...overrides,
  }
}

function renderDecisionDetail() {
  return render(
    <MemoryRouter initialEntries={['/decisions/ABC-D1']}>
      <Routes>
        <Route path="/decisions/:ref" element={<DecisionDetail />}>
          <Route index element={<DecisionOverview />} />
        </Route>
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
  listAttachments.mockResolvedValue({ attachments: [] })
  listComments.mockResolvedValue({ comments: [] })
})

// Regression test for a bug a code review caught: the diff view's five
// field blocks (title/context/decision/rationale/consequences) were
// hand copy-pasted, and "title" silently got dropped from the copy —
// the server returned diff.title but the UI never rendered it. This
// asserts every field's heading and changed line actually appear.
describe('DecisionDetail version history diff', () => {
  it('renders a diff block for every field, including title', async () => {
    const user = userEvent.setup()
    getDecision.mockResolvedValue(decision())
    const archivedVersion: DecisionVersion = {
      version: 1,
      title: 'v1 title',
      context: 'v1 context',
      decision: 'v1 decision',
      rationale: 'v1 rationale',
      consequences: 'v1 consequences',
      status: 'proposed',
      edited_by: 'human:alice',
      created_at: '2026-01-01T00:00:00Z',
    }
    listDecisionVersions.mockResolvedValue({ versions: [archivedVersion] })
    const diff: DecisionDiff = {
      from_version: 1,
      to_version: 2,
      title: [
        { op: 'remove', text: 'v1 title' },
        { op: 'add', text: 'v2 title' },
      ],
      context: [
        { op: 'remove', text: 'v1 context' },
        { op: 'add', text: 'v2 context' },
      ],
      decision: [
        { op: 'remove', text: 'v1 decision' },
        { op: 'add', text: 'v2 decision' },
      ],
      rationale: [
        { op: 'remove', text: 'v1 rationale' },
        { op: 'add', text: 'v2 rationale' },
      ],
      consequences: [
        { op: 'remove', text: 'v1 consequences' },
        { op: 'add', text: 'v2 consequences' },
      ],
      status_from: 'proposed',
      status_to: 'accepted',
    }
    getDecisionDiff.mockResolvedValue(diff)

    renderDecisionDetail()

    await waitFor(() => expect(screen.getByText('v1 title')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'Show diff' }))

    const diffSection = await waitFor(() => screen.getByTestId('version-diff'))
    for (const [label, oldText, newText] of [
      ['Title', 'v1 title', 'v2 title'],
      ['Context', 'v1 context', 'v2 context'],
      ['Decision', 'v1 decision', 'v2 decision'],
      ['Rationale', 'v1 rationale', 'v2 rationale'],
      ['Consequences', 'v1 consequences', 'v2 consequences'],
    ] as const) {
      expect(within(diffSection).getByRole('heading', { name: label })).toBeInTheDocument()
      expect(within(diffSection).getByText(new RegExp(oldText))).toBeInTheDocument()
      expect(within(diffSection).getByText(new RegExp(newText))).toBeInTheDocument()
    }
  })
})
