import { useState, type DragEvent } from 'react'
import type { Priority, WorkflowStatus } from '../api/types'

export interface BoardCard {
  ref: string
  status: WorkflowStatus
  priority: Priority
  version: number
}

export function useBoardDrag<T extends BoardCard>(
  onReorder: (card: T, afterRef: string | null) => void,
  onMove: (card: T, newStatus: WorkflowStatus) => void,
) {
  const [dragging, setDragging] = useState<T | null>(null)

  function dragProps(card: T) {
    return {
      draggable: true,
      onDragStart: () => setDragging(card),
      onDragEnd: () => setDragging(null),
    }
  }

  function columnDropProps(status: WorkflowStatus) {
    return {
      onDragOver: (e: DragEvent) => {
        if (dragging) e.preventDefault()
      },
      onDrop: (e: DragEvent) => {
        e.preventDefault()
        const dragged = dragging
        setDragging(null)
        if (dragged && dragged.status !== status) onMove(dragged, status)
      },
    }
  }

  function cardDropProps(target: T, columnCards: T[]) {
    return {
      onDragOver: (e: DragEvent) => {
        if (dragging) e.preventDefault()
      },
      onDrop: (e: DragEvent<HTMLElement>) => {
        e.preventDefault()
        e.stopPropagation()
        const dragged = dragging
        setDragging(null)
        if (!dragged || dragged.ref === target.ref) return
        if (dragged.status !== target.status) {
          onMove(dragged, target.status)
          return
        }
        if (dragged.priority !== target.priority) return
        const rect = e.currentTarget.getBoundingClientRect()
        const dropAfter = e.clientY - rect.top > rect.height / 2
        const ordered = columnCards.filter((c) => c.ref !== dragged.ref)
        const targetIdx = ordered.findIndex((c) => c.ref === target.ref)
        const afterRef = dropAfter
          ? target.ref
          : ordered[targetIdx - 1]?.priority === dragged.priority
            ? ordered[targetIdx - 1].ref
            : null
        onReorder(dragged, afterRef)
      },
    }
  }

  return { dragging, dragProps, columnDropProps, cardDropProps }
}
