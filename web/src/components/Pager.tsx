export function Pager({
  hasPrev,
  hasNext,
  loading,
  onPrev,
  onNext,
}: {
  hasPrev: boolean
  hasNext: boolean
  loading: boolean
  onPrev: () => void
  onNext: () => void
}) {
  if (!hasPrev && !hasNext) return null
  return (
    <p>
      <button onClick={onPrev} disabled={!hasPrev || loading}>
        Previous
      </button>{' '}
      <button onClick={onNext} disabled={!hasNext || loading}>
        Next
      </button>
    </p>
  )
}
