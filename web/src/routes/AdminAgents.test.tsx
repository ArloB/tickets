import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminAgents from './AdminAgents'
import * as agentsApi from '../api/agents'
import { useAuth } from '../auth/AuthContext'
import type { AgentDetail, AgentTokenSummary } from '../api/agents'

vi.mock('../api/agents', () => ({
  listAgents: vi.fn(),
  createAgent: vi.fn(),
  listAgentTokens: vi.fn(),
  createAgentToken: vi.fn(),
  revokeAgentToken: vi.fn(),
}))

vi.mock('../auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

const listAgents = vi.mocked(agentsApi.listAgents)
const createAgent = vi.mocked(agentsApi.createAgent)
const listAgentTokens = vi.mocked(agentsApi.listAgentTokens)
const createAgentToken = vi.mocked(agentsApi.createAgentToken)
const revokeAgentToken = vi.mocked(agentsApi.revokeAgentToken)
const mockUseAuth = vi.mocked(useAuth)

function agent(name: string, description = 'a worker agent'): AgentDetail {
  return { name, description, owner: 'human:admin', created_at: '2026-01-01T00:00:00Z' }
}

function token(id: number, description: string, revokedAt?: string): AgentTokenSummary {
  return { id, description, created_at: '2026-01-01T00:00:00Z', revoked_at: revokedAt }
}

function renderAdminAgents() {
  return render(
    <MemoryRouter initialEntries={['/admin/agents']}>
      <Routes>
        <Route path="/admin/agents" element={<AdminAgents />} />
        <Route path="/" element={<p>Home</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('AdminAgents', () => {
  it('redirects a non-admin away instead of rendering agent data', async () => {
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: false },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    listAgents.mockResolvedValueOnce({ agents: [agent('bot-1')] })

    renderAdminAgents()

    expect(await screen.findByText('Home')).toBeInTheDocument()
    expect(screen.queryByText('bot-1')).not.toBeInTheDocument()
  })

  it('lists agents, creates a new one, and shows a freshly created token exactly once', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: true },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    listAgents
      .mockResolvedValueOnce({ agents: [agent('bot-1')] })
      .mockResolvedValueOnce({ agents: [agent('bot-1'), agent('bot-2')] })
    createAgent.mockResolvedValueOnce(agent('bot-2'))
    listAgentTokens.mockResolvedValueOnce({ tokens: [] }).mockResolvedValueOnce({
      tokens: [token(1, 'ci pipeline')],
    })
    createAgentToken.mockResolvedValueOnce({
      id: 1,
      token: 'secret-raw-token-value',
      description: 'ci pipeline',
    })

    renderAdminAgents()

    expect(await screen.findByRole('button', { name: 'bot-1' })).toBeInTheDocument()

    await user.type(screen.getByLabelText('Name'), 'bot-2')
    await user.click(screen.getByRole('button', { name: 'Create agent' }))
    expect(await screen.findByRole('button', { name: 'bot-2' })).toBeInTheDocument()

    // Expand bot-1 to manage its tokens.
    await user.click(screen.getByRole('button', { name: 'bot-1' }))
    await waitFor(() => expect(listAgentTokens).toHaveBeenCalledWith('bot-1'))
    const bot1 = within(screen.getByRole('button', { name: 'bot-1' }).closest('li')!)
    expect(await bot1.findByText('No tokens yet.')).toBeInTheDocument()

    await user.type(bot1.getByLabelText('Description'), 'ci pipeline')
    await user.click(bot1.getByRole('button', { name: 'Create token' }))

    // The raw value must appear exactly once, in the one-time banner —
    // never as part of the persisted token list, which the component
    // never even has the data to render (AgentTokenSummary has no
    // `token` field).
    expect(await screen.findByText('This token will not be shown again.')).toBeInTheDocument()
    expect(screen.getByDisplayValue('secret-raw-token-value')).toBeInTheDocument()
    expect(await screen.findByText('ci pipeline')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText('This token will not be shown again.')).not.toBeInTheDocument()
    expect(screen.queryByDisplayValue('secret-raw-token-value')).not.toBeInTheDocument()
  })

  it('revokes a token and reflects the revoked state after refetch', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: true },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    listAgents.mockResolvedValueOnce({ agents: [agent('bot-1')] })
    listAgentTokens
      .mockResolvedValueOnce({ tokens: [token(1, 'ci pipeline')] })
      .mockResolvedValueOnce({ tokens: [token(1, 'ci pipeline', '2026-02-01T00:00:00Z')] })
    revokeAgentToken.mockResolvedValueOnce(undefined)

    renderAdminAgents()
    await user.click(await screen.findByRole('button', { name: 'bot-1' }))
    expect(await screen.findByRole('button', { name: 'Revoke' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Revoke' }))

    await waitFor(() => expect(revokeAgentToken).toHaveBeenCalledWith('bot-1', 1))
    expect(await screen.findByText('revoked 2026-02-01T00:00:00Z')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument()
  })
})
