import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import FeatureBoard from './FeatureBoard'
import * as featuresApi from '../api/features'
import { useAuth } from '../auth/AuthContext'
import type { FeatureCompact, FeaturesPage } from '../api/types'

vi.mock('../api/features', async () => {
  const actual = await vi.importActual<typeof import('../api/features')>('../api/features')
  return { ...actual, listFeatures: vi.fn(), reorderFeature: vi.fn(), updateFeatureStatus: vi.fn() }
})
vi.mock('../auth/AuthContext', () => ({ useAuth: vi.fn() }))

const listFeatures = vi.mocked(featuresApi.listFeatures)
const reorderFeature = vi.mocked(featuresApi.reorderFeature)
const mockUseAuth = vi.mocked(useAuth)

function compact(ref: string, priority: FeatureCompact['priority'] = 'medium'): FeatureCompact {
  return {
    ref,
    title: ref,
    status: 'backlog',
    priority,
    version: 1,
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function renderBoard() {
  return render(
    <MemoryRouter initialEntries={['/projects/ABC/features/board']}>
      <Routes>
        <Route path="/projects/:key/features/board" element={<FeatureBoard />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockUseAuth.mockReturnValue({
    me: { permission: 'editor', is_admin: false, actor: 'human:admin' },
    ready: true,
    bootstrapError: null,
    login: vi.fn(),
    logout: vi.fn(),
  })
})

describe('FeatureBoard reorder (mirrors Backlog.tsx)', () => {
  it('moves a feature down within its priority band and refetches that column', async () => {
    const user = userEvent.setup()
    const backlog: FeaturesPage = { features: [compact('ABC-F2'), compact('ABC-F3')] }
    listFeatures.mockImplementation(async (_key, filters) => {
      if (filters?.status === 'backlog') return backlog
      return { features: [] }
    })

    renderBoard()

    const downButtons = await screen.findAllByRole('button', { name: /Move .* down/ })
    const upButtons = screen.getAllByRole('button', { name: /Move .* up/ })
    expect(upButtons[0]).toBeDisabled()
    expect(downButtons[0]).not.toBeDisabled()

    await user.click(downButtons[0])

    expect(reorderFeature).toHaveBeenCalledWith('ABC-F2', 'ABC-F3', 1)
  })

  it('does not offer reorder across a priority-band boundary', async () => {
    listFeatures.mockImplementation(async (_key, filters) => {
      if (filters?.status === 'backlog') {
        return { features: [compact('ABC-F2', 'high'), compact('ABC-F3', 'low')] }
      }
      return { features: [] }
    })

    renderBoard()

    const downButtons = await screen.findAllByRole('button', { name: /Move .* down/ })
    expect(downButtons[0]).toBeDisabled()
  })
})
