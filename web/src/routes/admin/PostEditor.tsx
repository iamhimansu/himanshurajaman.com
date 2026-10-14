// Post editor: metadata controls (title/slug/date/summary/tags/published/cover)
// wrapped around an Editor.js body (loaded via EditorBody's dynamic import).
// Images upload through the Editor.js image tool (POST /api/admin/images); code
// blocks are a dedicated tool with a language selector + highlighting. Save
// serializes the Editor.js OutputData and POSTs (create) or PUTs (update, with
// fileSha). Every state is surfaced explicitly — a conflict/GitHub/validation
// error is shown, never faked as success.

import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  AdminApiError,
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  AdminWritesDisabledError,
  createPost,
  getPost,
  listPages,
  updatePost,
  uploadImage,
} from '../../lib/adminApi'
import type { AdminPageSummary, AdminPost, AdminPostMeta, AiCandidate } from '../../lib/types'
import { useAdminAuth } from './AdminApp'
import AiDraftPanel from './AiDraftPanel'
import EditorBody, { type EditorBodyHandle } from './EditorBody'
import type { OutputData } from './editor'
import {
  banner,
  btn,
  btnPrimary,
  errorBox,
  field,
  fieldLabel,
  input,
  linkButton,
  notice,
  subhead,
} from './ui'

interface PostEditorProps {
  mode: 'create' | 'edit'
}

interface FormState {
  title: string
  slug: string
  date: string
  summary: string
  tags: string[]
  page: string // "" => General (no owning page)
  published: boolean
  cover: string
}

function emptyForm(): FormState {
  return {
    title: '',
    slug: '',
    date: new Date().toISOString(),
    summary: '',
    tags: [],
    page: '',
    published: false,
    cover: '',
  }
}

function formFromPost(post: AdminPost): FormState {
  const m = post.meta
  return {
    title: m.title ?? '',
    slug: m.slug ?? post.slug,
    date: m.date ?? '',
    summary: m.summary ?? '',
    tags: m.tags ?? [],
    page: m.page ?? '',
    published: m.published ?? false,
    cover: m.cover ?? '',
  }
}

export default function PostEditor({ mode }: PostEditorProps) {
  const { slug: routeSlug } = useParams<{ slug: string }>()
  const [searchParams] = useSearchParams()
  const { writesEnabled, aiEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const bodyRef = useRef<EditorBodyHandle | null>(null)

  const readOnly = !writesEnabled

  const [form, setForm] = useState<FormState>(emptyForm)
  const [fileSha, setFileSha] = useState<string>('')
  // Existing page slugs for the "Page" selector (General + one per page).
  const [pages, setPages] = useState<AdminPageSummary[]>([])
  const [tagDraft, setTagDraft] = useState('')
  const [content, setContent] = useState<OutputData | undefined>(undefined)
  // Remount key for EditorBody so a conflict reload resets the editor contents.
  const [editorKey, setEditorKey] = useState(0)

  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'error'>(
    mode === 'edit' ? 'loading' : 'ready',
  )
  const [loadError, setLoadError] = useState('')

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saveNotice, setSaveNotice] = useState('')
  const [conflict, setConflict] = useState(false)

  useEffect(() => {
    document.title = mode === 'create' ? 'New post · Admin' : 'Edit post · Admin'
  }, [mode])

  // Load the existing post in edit mode.
  const loadPost = useCallback(
    (signal?: AbortSignal) => {
      if (mode !== 'edit' || !routeSlug) return
      setLoadState('loading')
      getPost(routeSlug, signal)
        .then((post) => {
          if (signal?.aborted) return
          setForm(formFromPost(post))
          setContent(post.content)
          setFileSha(post.fileSha)
          setEditorKey((k) => k + 1)
          setLoadState('ready')
        })
        .catch((err) => {
          if (signal?.aborted) return
          if (err instanceof AdminUnauthorizedError) return
          setLoadError(err instanceof Error ? err.message : 'Failed to load post')
          setLoadState('error')
        })
    },
    [mode, routeSlug],
  )

  useEffect(() => {
    const controller = new AbortController()
    loadPost(controller.signal)
    return () => controller.abort()
  }, [loadPost])

  // Load the page list for the selector. In create mode, pre-scope form.page
  // from ?page=<slug> once the list is known (only if that slug exists; else
  // fall back to General) so the nested "Create post" flow files the new post
  // under its page.
  useEffect(() => {
    const controller = new AbortController()
    listPages(controller.signal)
      .then((items) => {
        if (controller.signal.aborted) return
        setPages(items)
        if (mode === 'create') {
          const want = searchParams.get('page') ?? ''
          if (want && items.some((p) => p.slug === want)) {
            setForm((prev) => ({ ...prev, page: want }))
          }
        }
      })
      .catch(() => {
        // The selector just shows General only if the list can't be loaded.
      })
    return () => controller.abort()
  }, [mode, searchParams])

  const update = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }, [])

  const addTag = useCallback(() => {
    const t = tagDraft.trim().toLowerCase()
    if (!t) return
    setForm((prev) => (prev.tags.includes(t) ? prev : { ...prev, tags: [...prev.tags, t] }))
    setTagDraft('')
  }, [tagDraft])

  const removeTag = useCallback((tag: string) => {
    setForm((prev) => ({ ...prev, tags: prev.tags.filter((t) => t !== tag) }))
  }, [])

  // Apply an AI candidate: fill the metadata fields and load its blocks into the
  // editor. This NEVER saves — the author reviews, then Saves as usual. Prefer
  // the live instance (no flash); if it isn't ready yet, remount EditorBody with
  // the candidate as initial data via a changing key.
  const applyCandidate = useCallback((candidate: AiCandidate) => {
    setForm((prev) => ({
      ...prev,
      title: candidate.title,
      slug: candidate.slug || prev.slug,
      summary: candidate.summary,
      tags: candidate.tags,
    }))
    const remount = () => {
      setContent(candidate.content)
      setEditorKey((k) => k + 1)
    }
    const ed = bodyRef.current
    if (ed) {
      ed.setData(candidate.content).catch(remount)
    } else {
      remount()
    }
  }, [])

  const buildMeta = useCallback(
    (): AdminPostMeta => ({
      title: form.title,
      slug: form.slug,
      date: form.date,
      summary: form.summary,
      tags: form.tags,
      page: form.page,
      published: form.published,
      cover: form.cover,
    }),
    [form],
  )

  const onSave = useCallback(async () => {
    setSaving(true)
    setSaveError('')
    setSaveNotice('')
    setConflict(false)
    try {
      const doc = await bodyRef.current!.save()
      const meta = buildMeta()
      if (mode === 'create') {
        const res = await createPost({ meta, content: doc })
        navigate(`/admin/posts/${encodeURIComponent(res.slug)}/edit`)
        return
      }
      const res = await updatePost(form.slug, { meta, content: doc, fileSha })
      setFileSha(res.fileSha)
      setSaveNotice('Saved.')
    } catch (err) {
      if (err instanceof AdminStaleError) {
        setConflict(true)
        setSaveError(
          'This post changed on the server; reload to get the latest, then re-apply your edits.',
        )
      } else if (err instanceof AdminGitHubError) {
        setSaveError(`Couldn’t save to GitHub: ${err.message}`)
      } else if (err instanceof AdminWritesDisabledError) {
        setSaveError('This server is read-only (local content source).')
      } else if (err instanceof AdminWarmingError) {
        setSaveError('The server is still warming up; try again in a moment.')
      } else if (!(err instanceof AdminUnauthorizedError)) {
        setSaveError(err instanceof AdminApiError ? err.message : 'Save failed')
      }
    } finally {
      setSaving(false)
    }
  }, [mode, buildMeta, navigate, form.slug, fileSha])

  if (loadState === 'loading') {
    return (
      <div role="status" aria-live="polite">
        <p className="text-muted">Loading post…</p>
      </div>
    )
  }
  if (loadState === 'error') {
    return (
      <div role="alert">
        <p className="mb-1 font-semibold">Couldn’t load this post</p>
        <p className="text-muted">{loadError}</p>
        <p className="mt-4">
          <button type="button" className={btn} onClick={() => navigate('/admin/posts')}>
            Back to posts
          </button>
        </p>
      </div>
    )
  }

  return (
    <section className="max-w-3xl">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
        <h2 className="text-xl font-semibold">
          {mode === 'create' ? 'New post' : 'Edit post'}
        </h2>
        <div className="flex flex-wrap gap-2">
          <button type="button" className={btn} onClick={() => navigate('/admin/posts')}>
            Back
          </button>
          <button
            type="button"
            className={btnPrimary}
            disabled={readOnly || saving}
            onClick={() => void onSave()}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {readOnly && (
        <p className={`mb-4 ${banner}`} role="status">
          This server is read-only (local content source).
        </p>
      )}
      {saveNotice && (
        <p className={`mb-4 ${notice}`} role="status">
          {saveNotice}
        </p>
      )}
      {saveError && (
        <p className={`mb-4 ${errorBox}`} role="alert">
          {saveError}
          {conflict && (
            <>
              {' '}
              <button type="button" className={linkButton} onClick={() => loadPost()}>
                Reload
              </button>
            </>
          )}
        </p>
      )}

      {mode === 'create' && aiEnabled && !readOnly && (
        <AiDraftPanel onApply={applyCandidate} />
      )}

      <div className="flex flex-col gap-4">
        <label className={field}>
          <span className={fieldLabel}>Title</span>
          <input
            type="text"
            className={input}
            value={form.title}
            maxLength={200}
            disabled={readOnly}
            onChange={(e) => update('title', e.target.value)}
          />
        </label>

        <label className={field}>
          <span className={fieldLabel}>Slug</span>
          <input
            type="text"
            className={input}
            value={form.slug}
            maxLength={100}
            disabled={readOnly || mode === 'edit'}
            placeholder="my-post-slug"
            onChange={(e) => update('slug', e.target.value)}
          />
          {mode === 'edit' && (
            <small className="text-muted">Slug can’t change after creation.</small>
          )}
        </label>

        <label className={field}>
          <span className={fieldLabel}>Date</span>
          <input
            type="text"
            className={input}
            value={form.date}
            disabled={readOnly}
            placeholder="2024-03-02T09:00:00Z"
            onChange={(e) => update('date', e.target.value)}
          />
        </label>

        <label className={field}>
          <span className={fieldLabel}>Summary</span>
          <textarea
            className={input}
            value={form.summary}
            maxLength={500}
            rows={2}
            disabled={readOnly}
            onChange={(e) => update('summary', e.target.value)}
          />
        </label>

        <div className={field}>
          <span className={fieldLabel}>Tags</span>
          <div className="flex flex-wrap items-center gap-2 border border-line p-2">
            {form.tags.map((tag) => (
              <span
                className="inline-flex items-center gap-1.5 bg-[#f5f5f4] px-2 py-0.5 font-mono text-xs"
                key={tag}
              >
                {tag}
                {!readOnly && (
                  <button
                    type="button"
                    className="leading-none text-muted hover:text-text"
                    aria-label={`Remove ${tag}`}
                    onClick={() => removeTag(tag)}
                  >
                    ×
                  </button>
                )}
              </span>
            ))}
            {!readOnly && (
              <input
                type="text"
                className="min-w-[120px] flex-1 bg-transparent text-sm outline-none"
                value={tagDraft}
                placeholder="Add tag"
                onChange={(e) => setTagDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ',') {
                    e.preventDefault()
                    addTag()
                  }
                }}
                onBlur={addTag}
              />
            )}
          </div>
        </div>

        <label className={field}>
          <span className={fieldLabel}>Cover</span>
          <input
            type="text"
            className={input}
            value={form.cover}
            disabled={readOnly}
            placeholder="images/cover.png"
            onChange={(e) => update('cover', e.target.value)}
          />
        </label>

        <label className={field}>
          <span className={fieldLabel}>Page</span>
          <select
            className={input}
            value={form.page}
            disabled={readOnly}
            onChange={(e) => update('page', e.target.value)}
          >
            <option value="">General (none)</option>
            {pages.map((p) => (
              <option key={p.slug} value={p.slug}>
                {p.title}
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={form.published}
            disabled={readOnly}
            onChange={(e) => update('published', e.target.checked)}
          />
          <span className="text-sm text-text">Published</span>
        </label>
      </div>

      <div className="mt-6">
        <span className={`mb-2 block ${subhead}`}>Content</span>
        <EditorBody
          key={editorKey}
          ref={bodyRef}
          initialData={content}
          readOnly={readOnly}
          uploadImage={uploadImage}
        />
      </div>
    </section>
  )
}
