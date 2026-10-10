// Tiny fetch wrapper over the backend's JSON API. All paths are RELATIVE
// (/api/..., /images/...) so the exact same code works in dev (Vite proxies to
// the Go server) and in prod (same origin, served from the Go binary).

/** A non-2xx response that is not the warming gate (e.g. 404, 500). */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/** The backend's readiness gate: 503 {"status":"warming"} until the first
 *  ingest finishes. Treated as retryable, not a hard failure. */
export class WarmingError extends Error {
  constructor() {
    super('content is warming up')
    this.name = 'WarmingError'
  }
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    signal,
    headers: { Accept: 'application/json' },
  })

  // 503 is the warming gate on /api and /images; let React Query retry it.
  if (res.status === 503) {
    throw new WarmingError()
  }

  if (!res.ok) {
    let message = `request failed (${res.status})`
    try {
      const body = (await res.json()) as { error?: string }
      if (body?.error) message = body.error
    } catch {
      // non-JSON error body; keep the generic message
    }
    throw new ApiError(res.status, message)
  }

  return (await res.json()) as T
}

/**
 * POST a contact-form submission to /api/contact. Resolves on 200; throws an
 * ApiError (carrying the status) otherwise. Unlike apiGet this does NOT treat
 * 503 as a warming retry — for contact a 503 means the form is unavailable.
 */
export async function postContact(
  payload: { name: string; email: string; message: string; website: string },
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api/contact', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(payload),
    signal,
  })

  if (res.ok) return

  let message = `request failed (${res.status})`
  try {
    const body = (await res.json()) as { error?: string }
    if (body?.error) message = body.error
  } catch {
    // non-JSON error body; keep the generic message
  }
  throw new ApiError(res.status, message)
}

/** Build a query string from defined, non-empty params. */
export function qs(params: Record<string, string | number | undefined | null>): string {
  const sp = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue
    sp.set(key, String(value))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

/**
 * Map a content-relative image path (e.g. "images/avatar.png") to the served
 * URL ("/images/avatar.png"). Absolute http(s) URLs are returned unchanged.
 */
export function imageUrl(path?: string | null): string | undefined {
  if (!path) return undefined
  if (/^https?:\/\//i.test(path)) return path
  return '/' + path.replace(/^\/+/, '')
}
