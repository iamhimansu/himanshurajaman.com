// Post editor: metadata controls (title/slug/date/summary/tags/published/cover)
// wrapped around an Editor.js body (loaded via EditorBody's dynamic import).
// Images upload through the Editor.js image tool (POST /api/admin/images); code
// blocks are a dedicated tool with a language selector + highlighting. Save
// serializes the Editor.js OutputData and POSTs (create) or PUTs (update, with
// fileSha). Every state is surfaced explicitly — a conflict/GitHub/validation
// error is shown, never faked as success.

import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import {
  AdminApiError,
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  AdminWritesDisabledError,
  createPost,
  getPost,
  updatePost,
  uploadImage,
} from '../../lib/adminApi'
import type { AdminPost, AdminPostMeta } from '../../lib/types'
import { useAdminAuth } from './AdminApp'
import EditorBody, { type EditorBodyHandle } from './EditorBody'
import type { OutputData } from './editor'

interface PostEditorProps {
  mode: 'create' | 'edit'
}

interface FormState {
  title: string
  slug: string
  date: string
  summary: string
  tags: string[]
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
    published: m.published ?? false,
    cover: m.cover ?? '',
  }
}

export default function PostEditor({ mode }: PostEditorProps) {
  const { slug: routeSlug } = useParams<{ slug: string }>()
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const bodyRef = useRef<EditorBodyHandle | null>(null)

  const readOnly = !writesEnabled

  const [form, setForm] = useState<FormState>(emptyForm)
  const [fileSha, setFileSha] = useState<string>('')
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

  const buildMeta = useCallback(
    (): AdminPostMeta => ({
      title: form.title,
      slug: form.slug,
      date: form.date,
      summary: form.summary,
      tags: form.tags,
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
      <div className="state" role="status" aria-live="polite">
        <p className="muted">Loading post…</p>
      </div>
    )
  }
  if (loadState === 'error') {
    return (
      <div className="state" role="alert">
        <p className="state-title">Couldn’t load this post</p>
        <p className="muted">{loadError}</p>
        <p>
          <button type="button" className="btn" onClick={() => navigate('/admin/posts')}>
            Back to posts
          </button>
        </p>
      </div>
    )
  }

  return (
    <section className="admin-editor">
      <div className="admin-bar">
        <h2>{mode === 'create' ? 'New post' : 'Edit post'}</h2>
        <div className="admin-bar-actions">
          <button type="button" className="btn" onClick={() => navigate('/admin/posts')}>
            Back
          </button>
          <button
            type="button"
            className="btn primary"
            disabled={readOnly || saving}
            onClick={() => void onSave()}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {readOnly && (
        <p className="admin-banner" role="status">
          This server is read-only (local content source).
        </p>
      )}
      {saveNotice && (
        <p className="admin-notice" role="status">
          {saveNotice}
        </p>
      )}
      {saveError && (
        <p className="admin-error" role="alert">
          {saveError}
          {conflict && (
            <>
              {' '}
              <button type="button" className="link-button" onClick={() => loadPost()}>
                Reload
              </button>
            </>
          )}
        </p>
      )}

      <div className="admin-form">
        <label className="admin-field">
          <span>Title</span>
          <input
            type="text"
            value={form.title}
            maxLength={200}
            disabled={readOnly}
            onChange={(e) => update('title', e.target.value)}
          />
        </label>

        <label className="admin-field">
          <span>Slug</span>
          <input
            type="text"
            value={form.slug}
            maxLength={100}
            disabled={readOnly || mode === 'edit'}
            placeholder="my-post-slug"
            onChange={(e) => update('slug', e.target.value)}
          />
          {mode === 'edit' && (
            <small className="muted">Slug can’t change after creation.</small>
          )}
        </label>

        <label className="admin-field">
          <span>Date</span>
          <input
            type="text"
            value={form.date}
            disabled={readOnly}
            placeholder="2024-03-02T09:00:00Z"
            onChange={(e) => update('date', e.target.value)}
          />
        </label>

        <label className="admin-field">
          <span>Summary</span>
          <textarea
            value={form.summary}
            maxLength={500}
            rows={2}
            disabled={readOnly}
            onChange={(e) => update('summary', e.target.value)}
          />
        </label>

        <div className="admin-field">
          <span>Tags</span>
          <div className="admin-chips">
            {form.tags.map((tag) => (
              <span className="admin-chip" key={tag}>
                {tag}
                {!readOnly && (
                  <button
                    type="button"
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
                className="admin-chip-input"
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

        <label className="admin-field">
          <span>Cover</span>
          <input
            type="text"
            value={form.cover}
            disabled={readOnly}
            placeholder="images/cover.png"
            onChange={(e) => update('cover', e.target.value)}
          />
        </label>

        <label className="admin-field admin-field-inline">
          <input
            type="checkbox"
            checked={form.published}
            disabled={readOnly}
            onChange={(e) => update('published', e.target.checked)}
          />
          <span>Published</span>
        </label>
      </div>

      <div className="admin-editor-body">
        <span className="admin-pane-label">Content</span>
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
