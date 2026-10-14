// Types mirror the Go backend's JSON responses exactly (see internal/store and
// internal/server). Field names use the backend's snake_case where the API does
// (post fields), and camelCase where the settings file does.
//
// OutputData is imported type-only, so it is erased at build time and never
// pulls @editorjs/editorjs into the visitor entry bundle (verified via the
// build chunk inspection). Only the admin lazy chunk loads Editor.js at runtime.
import type { OutputData } from '@editorjs/editorjs'

/** One card in /api/posts and /api/search `items`. */
export interface PostSummary {
  slug: string
  title: string
  summary: string
  published_at: string // RFC3339 UTC
  updated_at: string | null
  cover_image: string | null
  reading_minutes: number
  tags: string[]
}

/** Full post from /api/posts/{slug}. `html` is already sanitized server-side. */
export interface PostDetail extends PostSummary {
  html: string
}

/** Shared envelope for /api/posts and /api/search. */
export interface PostList {
  items: PostSummary[]
  nextCursor: string | null
}

/** The public page view payload from /api/pages/{slug}. `html` is already
 *  sanitized server-side (same guarantee as PostDetail.html). */
export interface PageDetail {
  slug: string
  title: string
  html: string
  summary: string | null
  updated: string | null
}

/** GET /api/pages/{slug}: a published page plus its published posts in one
 *  round-trip. `posts` is always a (possibly empty) array. */
export interface PageResponse {
  page: PageDetail
  posts: PostSummary[]
}

/** One entry in /api/tags `items` (published-post counts only). */
export interface TagCount {
  name: string
  slug: string
  count: number
}

export interface TagsResponse {
  items: TagCount[]
}

/** /api/settings is the whole settings object; every field is treated as
 *  optional because settings is a free-form map on the server. */
export interface SiteSettings {
  title?: string
  description?: string
  author?: string
  baseUrl?: string
  accent?: string
  nav?: NavLink[]
  social?: SocialLink[]
  defaultOgImage?: string
  postsPerPage?: number
  /** Server-computed (from config, not site.json): whether the contact form is
   *  available. The SPA shows the form when true, else a mailto fallback. */
  contactEnabled?: boolean
}

/** POST /api/contact body. `website` is a honeypot and must be empty. */
export interface ContactPayload {
  name: string
  email: string
  message: string
  website: string
}

export interface NavLink {
  label: string
  path: string
}

export interface SocialLink {
  label: string
  url: string
}

/** /api/resume: ordered sections, each an opaque JSON blob keyed by `kind`. */
export interface ResumeResponse {
  sections: ResumeSection[]
}

export interface ResumeSection {
  kind: string
  data: unknown // shape depends on `kind`; see the typed views below
}

export interface ResumeBasics {
  name?: string
  headline?: string
  label?: string
  email?: string
  location?: string
  summary?: string
  links?: SocialLink[]
}

export interface ExperienceItem {
  org?: string
  role?: string
  start?: string
  end?: string | null
  location?: string
  highlights?: string[]
}

export interface EducationItem {
  org?: string
  credential?: string
  start?: string
  end?: string | null
}

export interface SkillGroup {
  name?: string
  items?: string[]
}

export interface ProjectItem {
  name?: string
  tag?: string
  url?: string
  description?: string
}

/** Optional "all repositories" style link under a projects section. */
export interface SectionLink {
  label: string
  url: string
}

/** Generic view used to render unknown section kinds without crashing. */
export interface GenericSection {
  title?: string
  items?: unknown[]
  groups?: unknown[]
  link?: SectionLink
}

/* ---- Admin / CMS -------------------------------------------------------- */
/* These mirror the admin API (see internal/server/{auth,admin}_handlers.go and
 * internal/admin/service.go). They live in the admin lazy chunk via adminApi.ts
 * and routes/admin/**; nothing in the visitor entry imports them at runtime. */

/** GET /api/admin/me — identity + CSRF token + whether GitHub writes work and
 *  whether AI drafting is configured (server-side GROQ_API_KEY). */
export interface AdminMe {
  email: string
  csrfToken: string
  writesEnabled: boolean
  aiEnabled: boolean
}

/** One row in GET /api/admin/posts (drafts included). */
export interface AdminPostSummary {
  slug: string
  title: string
  summary: string
  published: boolean
  published_at: string
  updated_at: string | null
  tags: string[]
}

export interface AdminPostListResponse {
  items: AdminPostSummary[]
}

/** Editable metadata block of a post (mirrors the server's `meta`). */
export interface AdminPostMeta {
  title: string
  slug: string
  date: string // RFC3339
  updated?: string
  summary: string
  tags: string[]
  /** Owning page slug, or "" for the general blog. Always sent on write so the
   *  current assignment is never dropped (the server omits it when empty). */
  page: string
  published: boolean
  cover: string
}

/** GET /api/admin/posts/{slug} — full editor payload. `content` is the exact
 *  Editor.js document to load into the editor. */
export interface AdminPost {
  slug: string
  meta: AdminPostMeta
  content: OutputData
  fileSha: string
}

/** POST /api/admin/posts body (create). */
export interface AdminPostCreateRequest {
  meta: AdminPostMeta
  content: OutputData
}

/** PUT /api/admin/posts/{slug} body (update) — create fields + required sha. */
export interface AdminPostUpdateRequest extends AdminPostCreateRequest {
  fileSha: string
}

/** 201/200 from POST/PUT /api/admin/posts. */
export interface AdminPostWriteResponse {
  slug: string
  fileSha: string
}

/** 200 from DELETE /api/admin/posts/{slug}. */
export interface AdminDeleteResponse {
  deleted: boolean
}

/* ---- Admin / Pages ------------------------------------------------------ */
/* Mirror internal/server/admin_pages_handlers.go + internal/store/pages.go. */

/** One row in GET /api/admin/pages (drafts included). `post_count` is the
 *  number of posts filed under this page across ALL statuses. */
export interface AdminPageSummary {
  slug: string
  title: string
  nav: boolean
  published: boolean
  post_count: number
}

export interface AdminPageListResponse {
  items: AdminPageSummary[]
}

/** Editable metadata block of a page (mirrors the server's `meta`). `navOrder`
 *  is nullable: null leaves it absent (sorts last), a number pins the order. */
export interface AdminPageMeta {
  title: string
  slug: string
  nav: boolean
  navLabel: string
  navOrder: number | null
  published: boolean
  summary: string
  updated?: string
}

/** GET /api/admin/pages/{slug} — full editor payload. `content` is the exact
 *  Editor.js document to load into the editor. */
export interface AdminPage {
  slug: string
  meta: AdminPageMeta
  content: OutputData
  fileSha: string
}

/** POST /api/admin/pages body (create). */
export interface AdminPageCreateRequest {
  meta: AdminPageMeta
  content: OutputData
}

/** PUT /api/admin/pages/{slug} body (update) — create fields + required sha. */
export interface AdminPageUpdateRequest extends AdminPageCreateRequest {
  fileSha: string
}

/** 201/200 from POST/PUT /api/admin/pages. */
export interface AdminPageWriteResponse {
  slug: string
  fileSha: string
}

/** 201 from POST /api/admin/images. */
export interface AdminImageResponse {
  url: string
  name: string
  fileSha: string
}

/** POST /api/admin/preview request/response. */
export interface AdminPreviewRequest {
  content: OutputData
}
export interface AdminPreviewResponse {
  html: string
}

/** GET /api/admin/resume response. `resume` is the raw resume document. */
export interface AdminResumeResponse {
  resume: unknown
  fileSha: string
}

/** PUT /api/admin/resume request/response. */
export interface AdminResumeUpdateRequest {
  resume: unknown
  fileSha: string
}
export interface AdminResumeWriteResponse {
  fileSha: string
}

/* ---- AI drafting (admin-only, Groq-backed) ------------------------------ */

/** The optional input-language selector for a draft request. */
export type AiLanguage = 'auto' | 'en' | 'hi'

/** GET /api/admin/models — the selectable chat-model allowlist, the default
 *  (first entry), and the per-request cap (GROQ_MAX_MODELS). */
export interface AiModelsResponse {
  models: string[]
  default: string
  max: number
}

/** POST /api/admin/assist body: a brief, optional language, and 1..max models. */
export interface AiAssistRequest {
  input: string
  language: AiLanguage
  models: string[]
}

/** One per-model failure returned alongside any successful candidates. */
export interface AiModelError {
  model: string
  message: string
}

/** One generated draft, labelled by the model that produced it. `content` is a
 *  ready-to-load Editor.js document. */
export interface AiCandidate {
  model: string
  title: string
  summary: string
  slug: string
  tags: string[]
  content: OutputData
}

/** POST /api/admin/assist response: successful candidates + per-model errors.
 *  Partial success (some candidates, some errors) is normal. */
export interface AiAssistResponse {
  candidates: AiCandidate[]
  errors: AiModelError[]
}
