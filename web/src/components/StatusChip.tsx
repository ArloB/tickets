export function StatusChip({
  value,
  kind,
}: {
  value: string
  kind: 'status' | 'priority' | 'severity' | 'decision'
}) {
  return (
    <span className={`chip chip-${kind}`} data-value={value}>
      {value}
    </span>
  )
}
