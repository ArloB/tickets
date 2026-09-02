import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TicketDetail, { TicketOverview } from './TicketDetail'
import { ApiError } from '../api/client'
import * as ticketsApi from '../api/tickets'
import * as linksApi from '../api/links'
import * as associationsApi from '../api/associations'
import * as backlinksApi from '../api/backlinks'
import * as attachmentsApi from '../api/attachments'
import { useAuth } from '../auth/AuthContext'
import type { TicketDetail as TicketDetailDto } from '../api/types'

vi.mock('../api/tickets', async () => {
  const actual = await vi.importActual<typeof import('../api/tickets')>('../api/tickets')
  return { ...actual, getTicket: vi.fn(), restoreTicket: vi.fn() }
})
vi.mock('../api/links', () => ({ listLinks: vi.fn() }))
vi.mock('../api/associations', () => ({ listAssociations: vi.fn() }))
vi.mock('../api/backlinks', () => ({ listBacklinks: vi.fn() }))
vi.mock('../api/attachments', () => ({ listAttachments: vi.fn() }))
vi.mock('../auth/AuthContext', () => ({ useAuth: vi.fn() }))

const getTicket = vi.mocked(ticketsApi.getTicket)
const restoreTicket = vi.mocked(ticketsApi.restoreTicket)
const listLinks = vi.mocked(linksApi.listLinks)
const listAssociations = vi.mocked(associationsApi.listAssociations)
const listBacklinks = vi.mocked(backlinksApi.listBacklinks)
const listAttachments = vi.mocked(attachmentsApi.listAttachments)
const mockUseAuth = vi.mocked(useAuth)

const notFound = new ApiError(
  { code: 'not_found', message: 'ticket not found', field: null, correlation_id: 'c1', current_version: null },
  404,
)

function ticket(overrides: Partial<TicketDetailDto> = {}): TicketDetailDto {
  return {
    ref: 'ABC-1',
    project: 'ABC',
    feature: 'ABC-F1',
    type: 'task',
    title: 'Do the thing',
    description: 'Body',
    status: 'backlog',
    priority: 'medium',
    version: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderTicketDetail() {
  return render(
    <MemoryRouter initialEntries={['/tickets/ABC-1']}>
      <Routes>
        <Route path="/tickets/:ref" element={<TicketDetail />}>
          <Route index element={<TicketOverview />} />
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
})

describe('TicketDetail — soft-delete discovery (ADR 0013)', () => {
  it('offers Restore to an editor who reaches a deleted ticket by ref', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: false, actor: 'human:admin' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    getTicket
      .mockRejectedValueOnce(notFound)
      .mockResolvedValueOnce(ticket({ version: 2, deleted_at: '2026-02-01T00:00:00Z' }))
    restoreTicket.mockResolvedValueOnce(ticket({ version: 3 }))
    getTicket.mockResolvedValueOnce(ticket({ version: 3 }))

    renderTicketDetail()

    expect(await screen.findByText(/This ticket was deleted/)).toBeInTheDocument()
    expect(getTicket).toHaveBeenNthCalledWith(2, 'ABC-1', [], true)

    await user.click(screen.getByRole('button', { name: 'Restore' }))

    expect(restoreTicket).toHaveBeenCalledWith('ABC-1', 2)
    expect(await screen.findByRole('heading', { name: /Do the thing/ })).toBeInTheDocument()
  })

  it('does not offer Restore to a non-editor', async () => {
    mockUseAuth.mockReturnValue({
      me: { permission: 'viewer', is_admin: false },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    getTicket
      .mockRejectedValueOnce(notFound)
      .mockResolvedValueOnce(ticket({ version: 2, deleted_at: '2026-02-01T00:00:00Z' }))

    renderTicketDetail()

    expect(await screen.findByText(/This ticket was deleted/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Restore' })).not.toBeInTheDocument()
  })

  it('falls back to the original not-found message when the ticket truly does not exist', async () => {
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: false, actor: 'human:admin' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    getTicket.mockRejectedValueOnce(notFound).mockRejectedValueOnce(notFound)

    renderTicketDetail()

    expect(await screen.findByText('ticket not found')).toBeInTheDocument()
  })
})
