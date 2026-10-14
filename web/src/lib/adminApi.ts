// Admin fetch wrapper. Lives in the admin lazy chunk (imported only from
// web/src/routes/admin/**). It attaches the double-submit CSRF header on every
// write, and translates the admin API's status codes into typed errors the UI
// can surface explicitly (never a faked success):
//
//   401                     -> AdminUnauthorizedError (guard redirects to login)
//   403 {error:"csrf"}      -> refresh /api/admin/me for a fresh token, retry ONCE
//   409 {error:"stale"}     -> AdminStaleError (carries currentFileSha)
//   502                     -> AdminGitHubError (GitHub write failed: <message>)
//   503 {error:"writes disabled…"}         -> AdminWritesDisabledError
//   503 {status:"warming"}                 -> AdminWarmingError (retryable)  [DR8]
//   503 {error:"auth temporarily unavailable"} -> AdminAuthUnavailableError  [DR8]
//   503 {error:"admin disabled"}           -> AdminDisabledError
//
// All paths are relative so dev (Vite proxy) and prod (same origin) match.

import type { OutputData } from '@editorjs/editorjs'

import type {
  AdminDeleteResponse,
  AdminImageResponse,
  AdminMe,
  AdminPage,
  AdminPageCreateRequest,
  AdminPageListResponse,
  AdminPageSummary,
  AdminPageUpdateRequest,
  AdminPageWriteResponse,
  AdminPost,
  AdminPostCreateRequest,
  AdminPostListResponse,
  AdminPostSummary,
  AdminPostUpdateRequest,
  AdminPostWriteResponse,
  AdminPreviewResponse,
  AdminResumeResponse,
  AdminResumeUpdateRequest,
  AdminResumeWriteResponse,
  AiAssistRequest,
  AiAssistResponse,
  AiModelError,
  AiModelsResponse,
} from './types'

/* ---- Typed errors ------------------------------------------------------- */

/** Any admin response the caller should surface with a message. */
export class AdminApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'AdminApiError'
    this.status = status
  }
}

/** 401 — no / invalid session. The guard redirects to /admin/login. */
export class AdminUnauthorizedError extends AdminApiError {
  constructor(message = 'unauthorized') {
    super(401, message)
    this.name = 'AdminUnauthorizedError'
  }
}

/** 409 {error:"page already exists"} — create collided with an existing page
 *  slug. Distinct from AdminStaleError (409 {error:"stale"}). */
export class AdminPageExistsError extends AdminApiError {
  constructor(message = 'page already exists') {
    super(409, message)
    this.name = 'AdminPageExistsError'
  }
}

/** 409 {error:"stale"} — the file changed on the server. */
export class AdminStaleError extends AdminApiError {
  currentFileSha: string
  constructor(currentFileSha: string) {
    super(409, 'stale')
    this.name = 'AdminStaleError'
    this.currentFileSha = currentFileSha
  }
}

/** 502 — the GitHub write failed; `message` is the server-provided reason. */
export class AdminGitHubError extends AdminApiError {
  constructor(message: string) {
    super(502, message)
    this.name = 'AdminGitHubError'
  }
}

/** 503 {error:"writes disabled…"} — content source is not GitHub. */
export class AdminWritesDisabledError extends AdminApiError {
  constructor(message = 'writes disabled: content source is not github') {
    super(503, message)
    this.name = 'AdminWritesDisabledError'
  }
}

/** 503 {status:"warming"} — the read cache is still warming up. [DR8] */
export class AdminWarmingError extends AdminApiError {
  constructor() {
    super(503, 'content is warming up')
    this.name = 'AdminWarmingError'
  }
}

/** 503 {error:"auth temporarily unavailable"} — Google discovery/JWKS down. [DR8] */
export class AdminAuthUnavailableError extends AdminApiError {
  constructor() {
    super(503, 'auth temporarily unavailable')
    this.name = 'AdminAuthUnavailableError'
  }
}

/** 503 {error:"admin disabled"} — admin is not configured on this server. */
export class AdminDisabledError extends AdminApiError {
  constructor() {
    super(503, 'admin disabled')
    this.name = 'AdminDisabledError'
  }
}

/** 503 — AI drafting is not configured on this server (no GROQ_API_KEY). */
export class AiUnavailableError extends AdminApiError {
  constructor(message = 'AI drafting is not available on this server') {
    super(503, message)
    this.name = 'AiUnavailableError'
  }
}

/** 502 — every selected model failed; `errors` carries the per-model reasons. */
export class AiAllFailedError extends AdminApiError {
  errors: AiModelError[]
  constructor(errors: AiModelError[]) {
    super(502, 'all selected models failed')
    this.name = 'AiAllFailedError'
    this.errors = errors
  }
}

/** 400 — invalid assist request (empty brief, bad model, over the model cap…). */
export class AiBadRequestError extends AdminApiError {
  constructor(message: string) {
    super(400, message)
    this.name = 'AiBadRequestError'
  }
}

/* ---- Wiring from AdminApp ------------------------------------------------ */

interface AdminApiConfig {
  /** Current in-memory CSRF token (from the last /api/admin/me). */
  getCsrfToken: () => string
  /** Re-fetch /api/admin/me, store the fresh token, and return it. */
  refreshCsrfToken: () => Promise<string>
  /** Called on a 401 so the guard can redirect to /admin/login. */
  onUnauthorized: () => void
}

let config: AdminApiConfig = {
  getCsrfToken: () => '',
  refreshCsrfToken: async () => '',
  onUnauthorized: () => {},
}

/** AdminApp calls this once it has an auth context. */
export function configureAdminApi(c: AdminApiConfig): void {
  config = c
}

/* ---- Core request ------------------------------------------------------- */

const WRITE_METHODS = new Set(['POST', 'PUT', 'DELETE'])

interface RequestOptions {
  method?: string
  json?: unknown
  form?: FormData
  signal?: AbortSignal
}

async function readError(res: Response): Promise<{ error?: string; status?: string; currentFileSha?: string }> {
  try {
    return (await res.json()) as { error?: string; status?: string; currentFileSha?: string }
  } catch {
    return {}
  }
}

/** Throw the right typed error for a non-2xx admin response. */
async function throwForStatus(res: Response): Promise<never> {
  const body = await readError(res)
  const message = body.error || `request failed (${res.status})`

  if (res.status === 401) {
    config.onUnauthorized()
    throw new AdminUnauthorizedError(message)
  }
  if (res.status === 409 && body.error === 'stale') {
    throw new AdminStaleError(body.currentFileSha ?? '')
  }
  if (res.status === 409 && body.error === 'page already exists') {
    throw new AdminPageExistsError(message)
  }
  if (res.status === 502) {
    throw new AdminGitHubError(message)
  }
  if (res.status === 503) {
    if (body.status === 'warming') throw new AdminWarmingError()
    if (body.error === 'admin disabled') throw new AdminDisabledError()
    if (body.error === 'auth temporarily unavailable') throw new AdminAuthUnavailableError()
    // writes-disabled or any other 503 the server surfaced with a message.
    throw new AdminWritesDisabledError(message)
  }
  throw new AdminApiError(res.status, message)
}

async function doFetch(path: string, opts: RequestOptions): Promise<Response> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (WRITE_METHODS.has(method)) {
    headers['X-CSRF-Token'] = config.getCsrfToken()
  }
  let bodyInit: BodyInit | undefined
  if (opts.form) {
    bodyInit = opts.form // browser sets multipart boundary
  } else if (opts.json !== undefined) {
    headers['Content-Type'] = 'application/json'
    bodyInit = JSON.stringify(opts.json)
  }
  return fetch(path, { method, headers, body: bodyInit, signal: opts.signal })
}

/**
 * Fetch with the double-submit CSRF retry: on 403 {error:"csrf"} refresh the
 * token from /api/admin/me and retry exactly once. A 403 for any other reason
 * throws immediately. The returned Response is otherwise unexamined, so callers
 * can map remaining status codes to their own typed errors.
 */
async function fetchWithCsrfRetry(path: string, opts: RequestOptions): Promise<Response> {
  let res = await doFetch(path, opts)
  if (res.status === 403) {
    const body = await readError(res)
    if (body.error === 'csrf') {
      await config.refreshCsrfToken() // reissues cookie + token
      res = await doFetch(path, opts) // retry once with the fresh token
    } else {
      throw new AdminApiError(403, body.error || 'forbidden')
    }
  }
  return res
}

/**
 * Perform an admin request and parse JSON. On 403 csrf, refresh the token from
 * /api/admin/me and retry exactly once. Any non-2xx is translated to a typed
 * error (see throwForStatus).
 */
async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await fetchWithCsrfRetry(path, opts)
  if (!res.ok) {
    await throwForStatus(res)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

/* ---- /api/admin/me (special: used by the guard + token refresh) --------- */

/**
 * GET /api/admin/me. 200 -> AdminMe; 401 -> AdminUnauthorizedError (does NOT
 * trigger the redirect callback, since the guard decides what to do); 503
 * admin-disabled -> AdminDisabledError. Does not send a CSRF header (GET).
 */
export async function fetchMe(signal?: AbortSignal): Promise<AdminMe> {
  const res = await fetch('/api/admin/me', {
    headers: { Accept: 'application/json' },
    signal,
  })
  if (res.ok) {
    return (await res.json()) as AdminMe
  }
  const body = await readError(res)
  if (res.status === 401) {
    throw new AdminUnauthorizedError(body.error || 'unauthorized')
  }
  if (res.status === 503) {
    if (body.error === 'admin disabled') throw new AdminDisabledError()
    if (body.error === 'auth temporarily unavailable') throw new AdminAuthUnavailableError()
    if (body.status === 'warming') throw new AdminWarmingError()
  }
  throw new AdminApiError(res.status, body.error || `request failed (${res.status})`)
}

/* ---- Posts -------------------------------------------------------------- */

export async function listPosts(
  status: 'all' | 'draft' | 'published' = 'all',
  page?: string,
  signal?: AbortSignal,
): Promise<AdminPostSummary[]> {
  // An empty/undefined page sends NO page filter, so the top-level Posts list
  // keeps returning every post. A non-empty page appends &page=<slug>.
  const path =
    `/api/admin/posts?status=${status}` +
    (page ? `&page=${encodeURIComponent(page)}` : '')
  const res = await request<AdminPostListResponse>(path, { signal })
  return res.items ?? []
}

export function getPost(slug: string, signal?: AbortSignal): Promise<AdminPost> {
  return request<AdminPost>(`/api/admin/posts/${encodeURIComponent(slug)}`, { signal })
}

export function createPost(body: AdminPostCreateRequest): Promise<AdminPostWriteResponse> {
  return request<AdminPostWriteResponse>('/api/admin/posts', { method: 'POST', json: body })
}

export function updatePost(
  slug: string,
  body: AdminPostUpdateRequest,
): Promise<AdminPostWriteResponse> {
  return request<AdminPostWriteResponse>(`/api/admin/posts/${encodeURIComponent(slug)}`, {
    method: 'PUT',
    json: body,
  })
}

export function deletePost(slug: string, fileSha?: string): Promise<AdminDeleteResponse> {
  // Omit fileSha when we don't have one: the server falls back to the cached
  // blob sha. Sending an empty string would fail the ^[0-9a-f]{40}$ check.
  const json = fileSha ? { fileSha } : {}
  return request<AdminDeleteResponse>(`/api/admin/posts/${encodeURIComponent(slug)}`, {
    method: 'DELETE',
    json,
  })
}

/* ---- Pages -------------------------------------------------------------- */

export async function listPages(signal?: AbortSignal): Promise<AdminPageSummary[]> {
  const res = await request<AdminPageListResponse>('/api/admin/pages', { signal })
  return res.items ?? []
}

export function getPage(slug: string, signal?: AbortSignal): Promise<AdminPage> {
  return request<AdminPage>(`/api/admin/pages/${encodeURIComponent(slug)}`, { signal })
}

export function createPage(body: AdminPageCreateRequest): Promise<AdminPageWriteResponse> {
  return request<AdminPageWriteResponse>('/api/admin/pages', { method: 'POST', json: body })
}

export function updatePage(
  slug: string,
  body: AdminPageUpdateRequest,
): Promise<AdminPageWriteResponse> {
  return request<AdminPageWriteResponse>(`/api/admin/pages/${encodeURIComponent(slug)}`, {
    method: 'PUT',
    json: body,
  })
}

export function deletePage(slug: string, fileSha?: string): Promise<AdminDeleteResponse> {
  // Omit fileSha when we don't have one: the server falls back to the cached
  // blob sha. Sending an empty string would fail the ^[0-9a-f]{40}$ check.
  const json = fileSha ? { fileSha } : {}
  return request<AdminDeleteResponse>(`/api/admin/pages/${encodeURIComponent(slug)}`, {
    method: 'DELETE',
    json,
  })
}

/* ---- Preview + images --------------------------------------------------- */

export function preview(content: OutputData, signal?: AbortSignal): Promise<AdminPreviewResponse> {
  return request<AdminPreviewResponse>('/api/admin/preview', {
    method: 'POST',
    json: { content },
    signal,
  })
}

export function uploadImage(file: File): Promise<AdminImageResponse> {
  const form = new FormData()
  form.append('file', file)
  return request<AdminImageResponse>('/api/admin/images', { method: 'POST', form })
}

/* ---- Resume ------------------------------------------------------------- */

export function getResume(signal?: AbortSignal): Promise<AdminResumeResponse> {
  return request<AdminResumeResponse>('/api/admin/resume', { signal })
}

export function updateResume(body: AdminResumeUpdateRequest): Promise<AdminResumeWriteResponse> {
  return request<AdminResumeWriteResponse>('/api/admin/resume', { method: 'PUT', json: body })
}

/* ---- AI drafting -------------------------------------------------------- */

/**
 * GET /api/admin/models — the selectable model allowlist + default + cap. 503
 * maps to AiUnavailableError (AI not configured); other non-2xx fall through to
 * the shared typed errors (401 → login, etc.).
 */
export async function listAiModels(signal?: AbortSignal): Promise<AiModelsResponse> {
  const res = await fetchWithCsrfRetry('/api/admin/models', { signal })
  if (res.status === 503) {
    throw new AiUnavailableError((await readError(res)).error || undefined)
  }
  if (!res.ok) {
    await throwForStatus(res)
  }
  return (await res.json()) as AiModelsResponse
}

/**
 * POST /api/admin/assist — fan a brief out to the selected models and return one
 * candidate per model that succeeded plus per-model errors. Status mapping:
 *   503 -> AiUnavailableError, 502 -> AiAllFailedError (carries errors),
 *   400 -> AiBadRequestError; others fall through to the shared typed errors.
 */
export async function assistDraft(
  body: AiAssistRequest,
  signal?: AbortSignal,
): Promise<AiAssistResponse> {
  const res = await fetchWithCsrfRetry('/api/admin/assist', { method: 'POST', json: body, signal })
  if (res.status === 503) {
    throw new AiUnavailableError((await readError(res)).error || undefined)
  }
  if (res.status === 502) {
    const parsed = (await res.json().catch(() => ({}))) as Partial<AiAssistResponse>
    throw new AiAllFailedError(parsed.errors ?? [])
  }
  if (res.status === 400) {
    throw new AiBadRequestError((await readError(res)).error || 'invalid request')
  }
  if (!res.ok) {
    await throwForStatus(res)
  }
  return (await res.json()) as AiAssistResponse
}

/* ---- Logout ------------------------------------------------------------- */

export function logout(): Promise<void> {
  return request<void>('/auth/logout', { method: 'POST' })
}
