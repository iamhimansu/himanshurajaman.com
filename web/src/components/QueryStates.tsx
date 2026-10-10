// Lightweight, animation-free status components for the three async states.
// The warming state exists because the API returns 503 {"status":"warming"}
// until the backend's first content ingest finishes; React Query keeps retrying
// and we show this instead of an error.

import { WarmingError } from '../lib/api'

export function Warming() {
  return (
    <div className="state" role="status" aria-live="polite">
      <p className="state-title">Warming up…</p>
      <p className="muted">
        The server is loading content for the first time. This usually takes a
        few seconds.
      </p>
    </div>
  )
}

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="state" role="status" aria-live="polite">
      <p className="muted">{label}</p>
    </div>
  )
}

export function ErrorState({ message }: { message?: string }) {
  return (
    <div className="state" role="alert">
      <p className="state-title">Something went wrong</p>
      <p className="muted">{message ?? 'Please try again in a moment.'}</p>
    </div>
  )
}

/**
 * Decide which status component to show for a pending/error query. `isWarming`
 * is true when the last failed attempt was the backend's warming gate, so we
 * reassure instead of alarm.
 */
export function PendingState({ failureReason }: { failureReason: unknown }) {
  if (failureReason instanceof WarmingError) return <Warming />
  return <Loading />
}
