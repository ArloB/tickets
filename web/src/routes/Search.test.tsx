import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Search from './Search'
import * as searchApi from '../api/search'
import type { SearchPage } from '../api/types'

vi.mock('../api/search', () => ({ search: vi.fn() }))

const search = vi.mocked(searchApi.search)

function renderSearch(initialQuery: string) {
  return render(
    <MemoryRouter initialEntries={[`/search?q=${encodeURIComponent(initialQuery)}`]}>
      <Routes>
        <Route path="/search" element={<Search />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  search.mockReset()
})

describe('Search', () => {
  it('renders hits for the ?q= in the URL', async () => {
    const page: SearchPage = {
      hits: [
        { kind: 'ticket', ref: 'ABC-1', title: 'Reticulate the splines', snippet: '…reticulate…' },
        { kind: 'comment', ref: 'ABC-1', comment_id: 4, snippet: '…reticulation looks fixed…' },
      ],
    }
    search.mockResolvedValue(page)

    renderSearch('reticulate')

    await waitFor(() => expect(search).toHaveBeenCalledWith('reticulate'))
    expect(await screen.findByText('Reticulate the splines')).toBeInTheDocument()
    expect(screen.getAllByText('ABC-1')).toHaveLength(2)
  })

  it('shows a no-results message for an empty page', async () => {
    search.mockResolvedValue({ hits: [] })

    renderSearch('nothing-matches-this')

    expect(await screen.findByText(/No results for/)).toBeInTheDocument()
  })

  it('prompts for a query when none is given', () => {
    renderSearch('')
    expect(screen.getByText('Enter a search term above.')).toBeInTheDocument()
    expect(search).not.toHaveBeenCalled()
  })
})
