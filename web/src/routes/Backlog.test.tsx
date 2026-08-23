import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Backlog from './Backlog'
import { ApiError } from '../api/client'
import * as ticketsApi from '../api/tickets'
import { useAuth } from '../auth/AuthContext'
import type { TicketCompact, TicketDetail, TicketsPage } from '../api/types'

vi.mock('../api/tickets', async () => {
  const actual = await vi.importActual<typeof import('../api/tickets')>('../api/tickets')
  return {
    ...actual,
    listTickets: vi.fn(),
    reorderTicket: vi.fn(),
    updateTicketStatus: vi.fn(),
    createTicket: vi.fn(),
  }
})

vi.mock('../auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

const listTickets = vi.mocked(ticketsApi.listTickets)
const reorderTicket = vi.mocked(ticketsApi.reorderTicket)
const updateTicketStatus = vi.mocked(ticketsApi.updateTicketStatus)
const mockUseAuth = vi.mocked(useAuth)

function compact(ref: string, title: string, priority: TicketCompact['priority']): TicketCompact {
  return {
    ref,
    title,
    type: 'task',
    status: 'backlog',
    priority,
    severity: undefined,
    updated_at: '2026-01-01T00:00:00Z',
    version: 1,
  }
}

function page(tickets: TicketCompact[]): TicketsPage {
  return { tickets }
}

function renderBacklog(initialPath = '/projects/DEMO/backlog') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/projects/:key/backlog" element={<Backlog />} />
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
})

function ticketRowRefs() {
  return screen.getAllByRole('row').slice(1).map((row) => within(row).getByRole('link').textContent)
}

describe('Backlog reorder', () => {
  it('moving a ticket up computes afterRef from the true predecessor, then trusts the refetch over the optimistic swap', async () => {
    const user = userEvent.setup()
    // B and C are adjacent critical-priority tickets; H1 is a
    // different-priority ticket that happens to render after them —
    // mirroring what a status filter can do to visible adjacency.
    const initial = [compact('DEMO-3', 'B', 'critical'), compact('DEMO-4', 'C', 'critical'), compact('DEMO-1', 'H1', 'high')]
    const afterReorder = [compact('DEMO-4', 'C', 'critical'), compact('DEMO-3', 'B', 'critical'), compact('DEMO-1', 'H1', 'high')]
    listTickets.mockResolvedValueOnce(page(initial)).mockResolvedValueOnce(page(afterReorder))
    reorderTicket.mockResolvedValueOnce({} as TicketDetail)

    renderBacklog()

    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-3', 'DEMO-4', 'DEMO-1']))

    await user.click(screen.getByRole('button', { name: 'Move DEMO-4 up' }))

    // C has no same-priority predecessor before B (B is the band head),
    // so afterRef must be null, not B's own ref.
    await waitFor(() => expect(reorderTicket).toHaveBeenCalledWith('DEMO-4', null, 1))
    // The move triggers a second listTickets call — the fix under test:
    // the displayed order comes from this refetch, not a local swap.
    await waitFor(() => expect(listTickets).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-4', 'DEMO-3', 'DEMO-1']))
  })

  it('moving a ticket down sends the immediate neighbor as afterRef', async () => {
    const user = userEvent.setup()
    const initial = [compact('DEMO-1', 'A', 'critical'), compact('DEMO-2', 'B', 'critical')]
    const afterReorder = [compact('DEMO-2', 'B', 'critical'), compact('DEMO-1', 'A', 'critical')]
    listTickets.mockResolvedValueOnce(page(initial)).mockResolvedValueOnce(page(afterReorder))
    reorderTicket.mockResolvedValueOnce({} as TicketDetail)

    renderBacklog()
    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-1', 'DEMO-2']))

    await user.click(screen.getByRole('button', { name: 'Move DEMO-1 down' }))

    await waitFor(() => expect(reorderTicket).toHaveBeenCalledWith('DEMO-1', 'DEMO-2', 1))
    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-2', 'DEMO-1']))
  })

  it('surfaces a reorder error without refetching or losing the current order', async () => {
    const user = userEvent.setup()
    const initial = [compact('DEMO-1', 'A', 'critical'), compact('DEMO-2', 'B', 'critical')]
    listTickets.mockResolvedValueOnce(page(initial))
    reorderTicket.mockRejectedValueOnce(
      new ApiError(
        {
          code: 'version_conflict',
          message: 'someone else moved this ticket',
          field: null,
          correlation_id: 'c1',
          current_version: 2,
        },
        409,
      ),
    )

    renderBacklog()
    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-1', 'DEMO-2']))

    await user.click(screen.getByRole('button', { name: 'Move DEMO-1 down' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('someone else moved this ticket')
    expect(listTickets).toHaveBeenCalledTimes(1)
    expect(ticketRowRefs()).toEqual(['DEMO-1', 'DEMO-2'])
  })
})

describe('Backlog bulk actions', () => {
  it('applies a bulk status change per-row, reporting partial failure and keeping the failed row selected', async () => {
    const user = userEvent.setup()
    const initial = [compact('DEMO-1', 'A', 'critical'), compact('DEMO-2', 'B', 'critical')]
    listTickets.mockResolvedValueOnce(page(initial))
    updateTicketStatus.mockImplementation(async (ref) => {
      if (ref === 'DEMO-1') {
        return { ...initial[0], project: 'DEMO', feature: 'DEMO-F1', description: '', creator: 'human:admin', created_at: '', status: 'done', version: 2 } as TicketDetail
      }
      throw new ApiError(
        { code: 'version_conflict', message: 'stale version', field: null, correlation_id: 'c2', current_version: 5 },
        409,
      )
    })

    renderBacklog()
    await waitFor(() => expect(ticketRowRefs()).toEqual(['DEMO-1', 'DEMO-2']))

    await user.click(screen.getByRole('checkbox', { name: 'Select DEMO-1' }))
    await user.click(screen.getByRole('checkbox', { name: 'Select DEMO-2' }))
    await user.click(screen.getByRole('button', { name: 'Apply to selected' }))

    await waitFor(() => expect(updateTicketStatus).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('DEMO-1: done')).toBeInTheDocument()
    expect(await screen.findByText('DEMO-2: failed — stale version')).toBeInTheDocument()

    // The succeeded row is cleared from selection; the failed one stays
    // selected so the user can retry it.
    expect(screen.getByRole('checkbox', { name: 'Select DEMO-1' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Select DEMO-2' })).toBeChecked()
  })
})
