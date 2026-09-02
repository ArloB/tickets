import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Setup from './Setup'
import * as authApi from '../api/auth'
import * as agentsApi from '../api/agents'
import * as adminApi from '../api/admin'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

vi.mock('../api/auth', () => ({
  setupAdmin: vi.fn(),
}))

vi.mock('../api/agents', () => ({
  createAgent: vi.fn(),
  createAgentToken: vi.fn(),
}))

vi.mock('../api/admin', () => ({
  setupImport: vi.fn(),
}))

vi.mock('../auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

const setupAdmin = vi.mocked(authApi.setupAdmin)
const createAgent = vi.mocked(agentsApi.createAgent)
const createAgentToken = vi.mocked(agentsApi.createAgentToken)
const setupImport = vi.mocked(adminApi.setupImport)
const mockUseAuth = vi.mocked(useAuth)

function renderSetup() {
  return render(
    <MemoryRouter initialEntries={['/setup']}>
      <Routes>
        <Route path="/setup" element={<Setup />} />
        <Route path="/" element={<p>Home</p>} />
        <Route path="/login" element={<p>Sign in page</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

async function renderSetupPastChoice(user: ReturnType<typeof userEvent.setup>) {
  const result = renderSetup()
  await user.click(screen.getByRole('button', { name: 'Start fresh' }))
  return result
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('Setup', () => {
  it('shows a choice between starting fresh and importing when there is no session', () => {
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderSetup()

    expect(screen.getByRole('button', { name: 'Start fresh' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore from an export' })).toBeInTheDocument()
  })

  it('shows the admin-creation form after choosing to start fresh', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    await renderSetupPastChoice(user)

    expect(screen.getByRole('heading', { name: 'Create the admin account' })).toBeInTheDocument()
  })

  it('creates the admin account then logs in with the same credentials', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(undefined)
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login,
      logout: vi.fn(),
    })
    setupAdmin.mockResolvedValueOnce({ actor: 'human:admin' })

    await renderSetupPastChoice(user)

    await user.type(screen.getByLabelText('Username'), 'admin')
    await user.type(screen.getByLabelText('Password'), 'a-real-password')
    await user.type(screen.getByLabelText('Confirm password'), 'a-real-password')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(setupAdmin).toHaveBeenCalledWith('admin', 'a-real-password')
    expect(login).toHaveBeenCalledWith('admin', 'a-real-password')
  })

  it('rejects mismatched passwords without calling the API', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    await renderSetupPastChoice(user)

    await user.type(screen.getByLabelText('Username'), 'admin')
    await user.type(screen.getByLabelText('Password'), 'a-real-password')
    await user.type(screen.getByLabelText('Confirm password'), 'does-not-match')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('Passwords do not match.')).toBeInTheDocument()
    expect(setupAdmin).not.toHaveBeenCalled()
  })

  it('shows a plain-language message and a sign-in link when setup already ran', async () => {
    const user = userEvent.setup()
    const login = vi.fn()
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login,
      logout: vi.fn(),
    })
    setupAdmin.mockRejectedValueOnce(
      new ApiError(
        {
          code: 'already_exists',
          message: 'a human account already exists; first-run setup only runs once',
          field: null,
          correlation_id: 'c1',
          current_version: null,
        },
        409,
      ),
    )

    await renderSetupPastChoice(user)

    await user.type(screen.getByLabelText('Username'), 'admin')
    await user.type(screen.getByLabelText('Password'), 'a-real-password')
    await user.type(screen.getByLabelText('Confirm password'), 'a-real-password')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This server already has an admin account. Sign in instead.',
    )
    expect(screen.getByRole('link', { name: 'Sign in instead' })).toHaveAttribute('href', '/login')
    expect(login).not.toHaveBeenCalled()
  })

  it('redirects a signed-in non-admin away rather than resuming the wizard', async () => {
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: false, actor: 'human:alice' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderSetup()

    expect(await screen.findByText('Home')).toBeInTheDocument()
  })

  it('does not treat an anonymous-viewer session as evidence setup already ran', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'viewer', is_admin: false },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    await renderSetupPastChoice(user)

    expect(screen.getByRole('heading', { name: 'Create the admin account' })).toBeInTheDocument()
  })

  it('tells the user their account exists even if the follow-up login fails', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new Error('network blip'))
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login,
      logout: vi.fn(),
    })
    setupAdmin.mockResolvedValueOnce({ actor: 'human:admin' })

    await renderSetupPastChoice(user)

    await user.type(screen.getByLabelText('Username'), 'admin')
    await user.type(screen.getByLabelText('Password'), 'a-real-password')
    await user.type(screen.getByLabelText('Confirm password'), 'a-real-password')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The admin account was created, but signing in failed. Sign in with the username and password you just set.',
    )
  })

  it('imports an export and proceeds to admin account creation on success', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    setupImport.mockResolvedValueOnce({ committed: true, counts: { projects: 1 }, problems: [] })

    renderSetup()
    await user.click(screen.getByRole('button', { name: 'Restore from an export' }))

    const envelope = new File(['{}'], 'export.json', { type: 'application/json' })
    await user.upload(screen.getByLabelText('Export file'), envelope)
    await user.click(screen.getByRole('button', { name: 'Import' }))

    await screen.findByRole('heading', { name: 'Create the admin account' })
    expect(setupImport).toHaveBeenCalledWith(envelope, undefined)
  })

  it('shows the reported problems and lets the user go back when import is refused', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: null,
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    setupImport.mockResolvedValueOnce({
      committed: false,
      counts: {},
      problems: ['target database is not empty'],
    })

    renderSetup()
    await user.click(screen.getByRole('button', { name: 'Restore from an export' }))
    await user.upload(
      screen.getByLabelText('Export file'),
      new File(['{}'], 'export.json', { type: 'application/json' }),
    )
    await user.click(screen.getByRole('button', { name: 'Import' }))

    expect(await screen.findByText('target database is not empty')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Back' }))
    expect(screen.getByRole('button', { name: 'Start fresh' })).toBeInTheDocument()
  })

  it('resumes at the token step for an admin session, then reaches the walkthrough', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: true, actor: 'human:admin' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderSetup()

    expect(screen.getByRole('heading', { name: 'Generate your first token' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Skip' }))
    expect(screen.getByRole('heading', { name: 'The fundamentals' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Finish' }))
    expect(await screen.findByText('Home')).toBeInTheDocument()
  })

  it('creates an agent and its first token, showing the raw value once', async () => {
    const user = userEvent.setup()
    mockUseAuth.mockReturnValue({
      me: { permission: 'editor', is_admin: true, actor: 'human:admin' },
      ready: true,
      bootstrapError: null,
      login: vi.fn(),
      logout: vi.fn(),
    })
    createAgent.mockResolvedValueOnce({
      name: 'cli',
      description: '',
      created_at: '2026-01-01T00:00:00Z',
    })
    createAgentToken.mockResolvedValueOnce({
      id: 1,
      token: 'secret-raw-token-value',
      description: 'created during first-run setup',
    })

    renderSetup()
    await user.click(screen.getByRole('button', { name: 'Create token' }))

    expect(await screen.findByText('This token will not be shown again.')).toBeInTheDocument()
    expect(screen.getByDisplayValue('secret-raw-token-value')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Continue' }))
    expect(screen.getByRole('heading', { name: 'The fundamentals' })).toBeInTheDocument()
  })
})
