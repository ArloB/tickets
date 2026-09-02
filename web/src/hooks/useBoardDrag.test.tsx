import { createEvent, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useBoardDrag, type BoardCard } from './useBoardDrag'

interface Card extends BoardCard {
  label: string
}

function Harness({
  cards,
  onReorder,
  onMove,
}: {
  cards: Record<string, Card[]>
  onReorder: (card: Card, afterRef: string | null) => void
  onMove: (card: Card, status: Card['status']) => void
}) {
  const { dragProps, columnDropProps, cardDropProps } = useBoardDrag<Card>(onReorder, onMove)
  return (
    <div>
      {Object.entries(cards).map(([status, list]) => (
        <div key={status} data-testid={`column-${status}`} {...columnDropProps(status as Card['status'])}>
          {list.map((c) => (
            <div key={c.ref} data-testid={c.ref} {...dragProps(c)} {...cardDropProps(c, list)}>
              {c.label}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

function card(ref: string, status: Card['status'], priority: Card['priority'] = 'medium'): Card {
  return { ref, status, priority, version: 1, label: ref }
}

describe('useBoardDrag', () => {
  it('moves a card to a different column when dropped there', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const cards = {
      backlog: [card('A', 'backlog')],
      done: [card('B', 'done')],
    }
    render(<Harness cards={cards} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('A'))
    fireEvent.drop(screen.getByTestId('B'))

    expect(onMove).toHaveBeenCalledWith(cards.backlog[0], 'done')
    expect(onReorder).not.toHaveBeenCalled()
  })

  it('moves a card dropped on an empty column background', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const cards = {
      backlog: [card('A', 'backlog')],
      done: [],
    }
    render(<Harness cards={cards} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('A'))
    fireEvent.drop(screen.getByTestId('column-done'))

    expect(onMove).toHaveBeenCalledWith(cards.backlog[0], 'done')
  })

  it('reorders within the same column when priorities match', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const list = [card('A', 'backlog', 'high'), card('B', 'backlog', 'high')]
    render(<Harness cards={{ backlog: list }} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('B'))
    fireEvent.drop(screen.getByTestId('A'))

    expect(onReorder).toHaveBeenCalledWith(list[1], null)
    expect(onMove).not.toHaveBeenCalled()
  })

  it('drops after the target when released past its midpoint', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const list = [card('A', 'backlog', 'high'), card('B', 'backlog', 'high')]
    render(<Harness cards={{ backlog: list }} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('B'))
    const target = screen.getByTestId('A')
    const dropEvent = createEvent.drop(target)
    Object.defineProperty(dropEvent, 'clientY', { value: 100 })
    fireEvent(target, dropEvent)

    expect(onReorder).toHaveBeenCalledWith(list[1], 'A')
  })

  it('refuses to reorder across a priority-band boundary', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const list = [card('A', 'backlog', 'high'), card('B', 'backlog', 'low')]
    render(<Harness cards={{ backlog: list }} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('B'))
    fireEvent.drop(screen.getByTestId('A'))

    expect(onReorder).not.toHaveBeenCalled()
    expect(onMove).not.toHaveBeenCalled()
  })

  it('is a no-op when a card is dropped on itself', () => {
    const onReorder = vi.fn()
    const onMove = vi.fn()
    const list = [card('A', 'backlog')]
    render(<Harness cards={{ backlog: list }} onReorder={onReorder} onMove={onMove} />)

    fireEvent.dragStart(screen.getByTestId('A'))
    fireEvent.drop(screen.getByTestId('A'))

    expect(onReorder).not.toHaveBeenCalled()
    expect(onMove).not.toHaveBeenCalled()
  })
})
