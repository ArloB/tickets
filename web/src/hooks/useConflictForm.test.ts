import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { useConflictForm, type FormFields, type Versioned } from './useConflictForm'

function conflictError(currentVersion: number): ApiError {
  return new ApiError(
    {
      code: 'version_conflict',
      message: 'stale',
      field: null,
      correlation_id: 'c1',
      current_version: currentVersion,
    },
    409,
  )
}

const baseFields: FormFields = { title: 'Original title', priority: 'medium' }

describe('useConflictForm', () => {
  it('saves cleanly when nothing conflicts', async () => {
    const save = vi.fn(
      async (fields: FormFields): Promise<Versioned<FormFields>> => ({ fields, version: 2 }),
    )
    const { result } = renderHook(() =>
      useConflictForm({
        base: baseFields,
        baseVersion: 1,
        fetchLive: async () => ({ fields: baseFields, version: 1 }),
        save,
      }),
    )

    act(() => result.current.setField('title', 'New title'))
    await act(async () => {
      await result.current.submit()
    })

    expect(save).toHaveBeenCalledWith({ title: 'New title', priority: 'medium' }, 1)
    expect(result.current.conflict).toBeNull()
    expect(result.current.draft.title).toBe('New title')
  })

  it('auto-applies a field the user never touched, and flags a real conflict for one both sides changed', async () => {
    const save = vi.fn().mockRejectedValueOnce(conflictError(2))
    const fetchLive = vi.fn(
      async (): Promise<Versioned<FormFields>> => ({
        fields: { title: "Someone else's title", priority: 'medium' },
        version: 2,
      }),
    )
    const { result } = renderHook(() =>
      useConflictForm({ base: baseFields, baseVersion: 1, fetchLive, save }),
    )

    // User only touches title; priority stays at the base value.
    act(() => result.current.setField('title', 'My title'))
    await act(async () => {
      await result.current.submit()
    })

    expect(result.current.conflict).not.toBeNull()
    expect(result.current.conflict?.serverVersion).toBe(2)
    expect(result.current.conflict?.autoApplied).toEqual([])
    expect(result.current.conflict?.fields).toEqual([
      { field: 'title', base: 'Original title', server: "Someone else's title", draft: 'My title' },
    ])
    // Draft keeps the user's own typed value until they resolve it.
    expect(result.current.draft.title).toBe('My title')
  })

  it('silently takes the server value for an untouched field and resubmits with no conflict banner', async () => {
    // No field needs a human decision (title untouched by either side,
    // priority only changed server-side) — the hook should merge and
    // resubmit on its own, never surfacing a conflict banner at all.
    const save = vi
      .fn<(fields: FormFields, version: number) => Promise<Versioned<FormFields>>>()
      .mockRejectedValueOnce(conflictError(2))
      .mockResolvedValueOnce({ fields: { title: 'Original title', priority: 'high' }, version: 3 })
    const fetchLive = vi.fn(
      async (): Promise<Versioned<FormFields>> => ({
        fields: { title: 'Original title', priority: 'high' },
        version: 2,
      }),
    )
    const { result } = renderHook(() =>
      useConflictForm({ base: baseFields, baseVersion: 1, fetchLive, save }),
    )

    await act(async () => {
      await result.current.submit()
    })

    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save).toHaveBeenNthCalledWith(2, { title: 'Original title', priority: 'high' }, 2)
    expect(result.current.conflict).toBeNull()
    expect(result.current.draft.priority).toBe('high')
  })

  it('resubmits automatically when the merge leaves no field needing a decision', async () => {
    const save = vi
      .fn<(fields: FormFields, version: number) => Promise<Versioned<FormFields>>>()
      .mockRejectedValueOnce(conflictError(2))
      .mockResolvedValueOnce({ fields: { title: 'My title', priority: 'high' }, version: 3 })
    const fetchLive = vi.fn(
      async (): Promise<Versioned<FormFields>> => ({
        fields: { title: 'Original title', priority: 'high' },
        version: 2,
      }),
    )
    const { result } = renderHook(() =>
      useConflictForm({ base: baseFields, baseVersion: 1, fetchLive, save }),
    )

    act(() => result.current.setField('title', 'My title'))
    await act(async () => {
      await result.current.submit()
    })

    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save).toHaveBeenNthCalledWith(2, { title: 'My title', priority: 'high' }, 2)
    expect(result.current.conflict).toBeNull()
  })

  it('resolveField clears one pending conflict and readyToResubmit flips once all are resolved', async () => {
    const save = vi.fn().mockRejectedValueOnce(conflictError(2))
    const fetchLive = vi.fn(
      async (): Promise<Versioned<FormFields>> => ({
        fields: { title: "Someone else's title", priority: 'low' },
        version: 2,
      }),
    )
    const { result } = renderHook(() =>
      useConflictForm({ base: baseFields, baseVersion: 1, fetchLive, save }),
    )

    act(() => result.current.setField('title', 'My title'))
    act(() => result.current.setField('priority', 'high'))
    await act(async () => {
      await result.current.submit()
    })

    expect(result.current.conflict?.fields).toHaveLength(2)
    expect(result.current.readyToResubmit).toBe(false)

    act(() => result.current.resolveField('title', 'My title'))
    expect(result.current.conflict?.fields).toHaveLength(1)
    expect(result.current.readyToResubmit).toBe(false)

    act(() => result.current.resolveField('priority', 'low'))
    expect(result.current.conflict?.fields).toHaveLength(0)
    expect(result.current.readyToResubmit).toBe(true)
    expect(result.current.draft.priority).toBe('low')
  })

  it('surfaces a non-conflict error without discarding the draft', async () => {
    const save = vi.fn().mockRejectedValueOnce(
      new ApiError(
        { code: 'validation_failed', message: 'title is required', field: 'title', correlation_id: 'c1', current_version: null },
        400,
      ),
    )
    const { result } = renderHook(() =>
      useConflictForm({
        base: baseFields,
        baseVersion: 1,
        fetchLive: async () => ({ fields: baseFields, version: 1 }),
        save,
      }),
    )

    act(() => result.current.setField('title', ''))
    await act(async () => {
      await result.current.submit()
    })

    expect(result.current.error).toBe('title is required')
    expect(result.current.conflict).toBeNull()
    expect(result.current.draft.title).toBe('')
  })
})
