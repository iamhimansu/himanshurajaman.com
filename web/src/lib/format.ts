// Small presentation helpers shared across routes.

/** "2024-03-02T09:00:00Z" -> "Mar 2, 2024". Falls back to the raw string. */
export function formatDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

/** "5 min read", with a sensible floor of 1 minute for any non-empty post. */
export function readingTime(minutes: number): string {
  const m = Math.max(1, Math.round(minutes))
  return `${m} min read`
}
