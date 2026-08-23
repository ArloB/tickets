import { useEffect, useState } from 'react'
import { ApiError } from '../api/client'

/** Every field this hook manages is a plain string on the wire
 * (enums, plain text, or '' standing in for an omitted optional like
 * ticket severity) — this is what lets the three-way merge below
 * compare values with `===` instead of a per-type comparator. */
export type FormFields = Record<string, string>

export interface Versioned<T> {
  fields: T
  version: number
}

export interface FieldConflict {
  field: string
  base: string
  server: string
  draft: string
}

export interface ConflictState {
  serverVersion: number
  fields: FieldConflict[]
  /** Fields the user never touched, silently updated to the server's
   * current value (plan §3 step 3) — shown for transparency, not for
   * a decision. */
  autoApplied: string[]
}

interface UseConflictFormArgs<T extends FormFields> {
  /** The record's field values as of the originating GET. */
  base: T
  baseVersion: number
  /** Re-fetches the live record after a 409, mapped to the same field
   * shape as `base` — never a cached/stale copy. */
  fetchLive: () => Promise<Versioned<T>>
  /** Issues the actual PUT/PATCH with `expectedVersion` as If-Match. */
  save: (values: T, expectedVersion: number) => Promise<Versioned<T>>
}

/** The Phase 4 exit criterion's conflict-resolution machinery (plan.md
 * §3), built once and reused across ticket/feature/decision edit
 * forms: keep the full base snapshot, not just a version number
 * (PUT/PATCH here are full-representation updates — resending the
 * same body with a fresher If-Match would silently clobber whatever
 * else changed); on 409, re-fetch and three-way-merge; a field the
 * user never touched takes the server's value automatically, a field
 * both sides changed differently needs the user's decision, and the
 * draft is never discarded on conflict. */
export function useConflictForm<T extends FormFields>({
  base,
  baseVersion,
  fetchLive,
  save,
}: UseConflictFormArgs<T>) {
  const [draft, setDraft] = useState<T>(base)
  const [baseline, setBaseline] = useState<T>(base)
  const [baselineVersion, setBaselineVersion] = useState(baseVersion)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<ConflictState | null>(null)

  // Resets whenever the caller loads a genuinely different base
  // record (a fresh GET — identified by its version, since a field
  // edit alone never changes baseVersion). Does NOT fire on our own
  // post-save/post-conflict baseline updates below, since those go
  // through setBaselineVersion, not a re-render with a new `base` prop.
  useEffect(() => {
    setDraft(base)
    setBaseline(base)
    setBaselineVersion(baseVersion)
    setConflict(null)
    setError(null)
    // `base`'s identity is caller-controlled (new object per fetch); only
    // baseVersion reliably signals "this is actually a different record load."
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [baseVersion])

  const dirty = Object.keys(baseline).some((k) => draft[k] !== baseline[k])

  function setField(key: keyof T, value: string) {
    setDraft((prev) => ({ ...prev, [key]: value }))
  }

  async function doSave(values: T, version: number): Promise<'ok' | 'conflict' | 'error'> {
    try {
      const result = await save(values, version)
      setBaseline(result.fields)
      setBaselineVersion(result.version)
      setDraft(result.fields)
      setConflict(null)
      return 'ok'
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict') {
        return 'conflict'
      }
      setError(err instanceof ApiError ? err.message : String(err))
      return 'error'
    }
  }

  async function submit() {
    setSaving(true)
    setError(null)
    const outcome = await doSave(draft, baselineVersion)
    if (outcome === 'conflict') {
      const live = await fetchLive()
      const server = live.fields
      const serverVersion = live.version
      const fields: FieldConflict[] = []
      const autoApplied: string[] = []
      const merged: FormFields = { ...draft }

      for (const key of Object.keys(baseline)) {
        const b = baseline[key]
        const s = server[key]
        const d = draft[key]
        if (s === b) continue // server didn't touch this field
        if (d === b) {
          merged[key] = s
          autoApplied.push(key)
        } else if (d !== s) {
          fields.push({ field: key, base: b, server: s, draft: d })
        }
        // d === s: both sides changed to the same value — already resolved
      }

      setDraft(merged as T)
      setBaseline(server)
      setBaselineVersion(serverVersion)

      if (fields.length > 0) {
        setConflict({ serverVersion, fields, autoApplied })
      } else {
        // Nothing needs a human decision — resubmit against the now-
        // current version rather than leaving the user's click inert.
        await doSave(merged as T, serverVersion)
      }
    }
    setSaving(false)
  }

  /** Records the user's choice for one conflicting field and clears it
   * from the pending list. `value` is typically `field.draft` (keep
   * mine) or `field.server` (take theirs), but any string is valid —
   * the field is free text. */
  function resolveField(field: string, value: string) {
    setDraft((prev) => ({ ...prev, [field]: value }))
    setConflict((prev) => prev && { ...prev, fields: prev.fields.filter((f) => f.field !== field) })
  }

  return {
    draft,
    setField,
    dirty,
    saving,
    error,
    conflict,
    submit,
    resolveField,
    /** True once every conflicting field has a resolved value — the
     * caller should re-enable its Save button (this is what triggers
     * plan §3 step 4's "resubmits with the merged body"). */
    readyToResubmit: conflict !== null && conflict.fields.length === 0,
  }
}
