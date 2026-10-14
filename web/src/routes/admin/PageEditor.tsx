// Page editor: metadata controls (title/slug/nav/navLabel/navOrder/summary/
// published) wrapped around an Editor.js body (loaded via EditorBody's dynamic
// import). Save serializes the Editor.js OutputData and POSTs (create) or PUTs
// (update, with fileSha). In edit mode a nested posts gridview lists the posts
// filed under this page (meta.page == slug) with Edit/Delete + a "Create post"
// button pre-scoped to this page. Every state is surfaced explicitly — a
// conflict/GitHub/validation error is shown, never faked as success.
//
// Mirrors PostEditor.tsx; all imports are admin-chunk only (adminApi,
// EditorBody) so the visitor bundle is untouched.

import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import {
  AdminApiError,
  AdminGitHubError,
  AdminPageExistsError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  AdminWritesDisabledError,
  createPage,
  deletePost,
  getPage,
  listPosts,
  updatePage,
  uploadImage,
} from '../../lib/adminApi'
import type { AdminPage, AdminPageMeta, AdminPostSummary } from '../../lib/types'
import { isReservedSlug } from '../../lib/reserved'
import { formatDate } from '../../lib/format'
import { useAdminAuth } from './AdminApp'
import EditorBody, { type EditorBodyHandle } from './EditorBody'
import type { OutputData } from './editor'
import {
  badgeDraft,
  badgePublished,
  banner,
  btn,
  btnPrimary,
  errorBox,
  field,
  fieldLabel,
  input,
  linkButton,
  notice,
  rowHover,
  subhead,
  table,
  td,
  th,
} from './ui'

interface PageEditorProps {
  mode: 'create' | 'edit'
}

interface FormState {
  title: string
  slug: string
  nav: boolean
  navLabel: string
  navOrder: string // raw text; "" => absent (sorts last)
  published: boolean
  summary: string
}

function emptyForm(): FormState {
  return {
    title: '',
    slug: '',
    nav: false,
    navLabel: '',
    navOrder: '',
    published: false,
    summary: '',
  }
}

function formFromPage(page: AdminPage): FormState {
  const m = page.meta
  return {
    title: m.title ?? '',
    slug: m.slug ?? page.slug,
    nav: m.nav ?? false,
    navLabel: m.navLabel ?? '',
    navOrder: m.navOrder === null || m.navOrder === undefined ? '' : String(m.navOrder),
    published: m.published ?? false,
    summary: m.summary ?? '',
  }
}

export default function PageEditor({ mode }: PageEditorProps) {
  const { slug: routeSlug } = useParams<{ slug: string }>()
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const bodyRef = useRef<EditorBodyHandle | null>(null)

  const readOnly = !writesEnabled

  const [form, setForm] = useState<FormState>(emptyForm)
  const [fileSha, setFileSha] = useState<string>('')
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
    document.title = mode === 'create' ? 'New page · Admin' : 'Edit page · Admin'
  }, [mode])

  // Load the existing page in edit mode.
  const loadPage = useCallback(
    (signal?: AbortSignal) => {
      if (mode !== 'edit' || !routeSlug) return
      setLoadState('loading')
      getPage(routeSlug, signal)
        .then((page) => {
          if (signal?.aborted) return
          setForm(formFromPage(page))
          setContent(page.content)
          setFileSha(page.fileSha)
          setEditorKey((k) => k + 1)
          setLoadState('ready')
        })
        .catch((err) => {
          if (signal?.aborted) return
          if (err instanceof AdminUnauthorizedError) return
          setLoadError(err instanceof Error ? err.message : 'Failed to load page')
          setLoadState('error')
        })
    },
    [mode, routeSlug],
  )

  useEffect(() => {
    const controller = new AbortController()
    loadPage(controller.signal)
    return () => controller.abort()
  }, [loadPage])

  const update = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }, [])

  const buildMeta = useCallback((): AdminPageMeta => {
    const trimmed = form.navOrder.trim()
    const parsed = trimmed === '' ? null : Number(trimmed)
    const navOrder = parsed === null || Number.isNaN(parsed) ? null : parsed
    return {
      title: form.title,
      slug: form.slug,
      nav: form.nav,
      navLabel: form.navLabel,
      navOrder,
      published: form.published,
      summary: form.summary,
    }
  }, [form])

  const onSave = useCallback(async () => {
    setSaving(true)
    setSaveError('')
    setSaveNotice('')
    setConflict(false)

    // Client reserved-slug guard on create (defense in depth; the server also
    // 400s via ParsePage). Skip in edit mode where the slug is immutable.
    if (mode === 'create' && isReservedSlug(form.slug)) {
      setSaveError(`“${form.slug.trim()}” is a reserved slug; choose another.`)
      setSaving(false)
      return
    }

    try {
      const doc = await bodyRef.current!.save()
      const meta = buildMeta()
      if (mode === 'create') {
        const res = await createPage({ meta, content: doc })
        navigate(`/admin/pages/${encodeURIComponent(res.slug)}/edit`)
        return
      }
      const res = await updatePage(form.slug, { meta, content: doc, fileSha })
      setFileSha(res.fileSha)
      setSaveNotice('Saved.')
    } catch (err) {
      if (err instanceof AdminStaleError) {
        setConflict(true)
        setSaveError(
          'This page changed on the server; reload to get the latest, then re-apply your edits.',
        )
      } else if (err instanceof AdminPageExistsError) {
        setSaveError('A page with that slug already exists; choose another.')
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
        <p className="text-muted">Loading page…</p>
      </div>
    )
  }
  if (loadState === 'error') {
    return (
      <div role="alert">
        <p className="mb-1 font-semibold">Couldn’t load this page</p>
        <p className="text-muted">{loadError}</p>
        <p className="mt-4">
          <button type="button" className={btn} onClick={() => navigate('/admin/pages')}>
            Back to pages
          </button>
        </p>
      </div>
    )
  }

  return (
    <section className="max-w-3xl">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
        <h2 className="text-xl font-semibold">
          {mode === 'create' ? 'New page' : 'Edit page'}
        </h2>
        <div className="flex flex-wrap gap-2">
          <button type="button" className={btn} onClick={() => navigate('/admin/pages')}>
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
              <button type="button" className={linkButton} onClick={() => loadPage()}>
                Reload
              </button>
            </>
          )}
        </p>
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
            placeholder="my-page-slug"
            onChange={(e) => update('slug', e.target.value)}
          />
          {mode === 'edit' && (
            <small className="text-muted">Slug can’t change after creation.</small>
          )}
        </label>

        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={form.nav}
            disabled={readOnly}
            onChange={(e) => update('nav', e.target.checked)}
          />
          <span className="text-sm text-text">Show in top navigation</span>
        </label>

        <label className={field}>
          <span className={fieldLabel}>Nav label</span>
          <input
            type="text"
            className={input}
            value={form.navLabel}
            maxLength={200}
            disabled={readOnly}
            placeholder="Defaults to the title"
            onChange={(e) => update('navLabel', e.target.value)}
          />
        </label>

        <label className={field}>
          <span className={fieldLabel}>Nav order</span>
          <input
            type="number"
            className={input}
            value={form.navOrder}
            disabled={readOnly}
            placeholder="Lower sorts first; blank sorts last"
            onChange={(e) => update('navOrder', e.target.value)}
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

      {mode === 'edit' && <PagePosts pageSlug={form.slug} />}
    </section>
  )
}

// PagePosts is the nested posts gridview inside the page editor (edit mode): it
// lists posts filed under this page (meta.page == slug) via listPosts('all',
// slug). Columns: Title, Status, Date, Actions (Edit, Delete). "Create post"
// pre-scopes the post editor to this page via ?page=<slug>. Edit/Delete reuse
// the existing post flows.
function PagePosts({ pageSlug }: { pageSlug: string }) {
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const [posts, setPosts] = useState<AdminPostSummary[] | null>(null)
  const [status, setStatus] = useState<'loading' | 'warming' | 'ready' | 'error'>('loading')
  const [error, setError] = useState('')
  const [busySlug, setBusySlug] = useState<string | null>(null)
  const [confirmSlug, setConfirmSlug] = useState<string | null>(null)

  const load = useCallback(
    (signal?: AbortSignal) => {
      if (!pageSlug) return
      setStatus('loading')
      listPosts('all', pageSlug, signal)
        .then((items) => {
          if (signal?.aborted) return
          setPosts(items)
          setStatus('ready')
        })
        .catch((err) => {
          if (signal?.aborted) return
          if (err instanceof AdminUnauthorizedError) return
          if (err instanceof AdminWarmingError) {
            setStatus('warming')
            return
          }
          setError(err instanceof Error ? err.message : 'Failed to load posts')
          setStatus('error')
        })
    },
    [pageSlug],
  )

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  const onDelete = useCallback(
    async (post: AdminPostSummary) => {
      if (!writesEnabled) return
      setBusySlug(post.slug)
      setError('')
      try {
        await deletePost(post.slug)
        setConfirmSlug(null)
        load()
      } catch (err) {
        if (err instanceof AdminStaleError) {
          setError('This post changed on the server; refresh and try again.')
        } else if (err instanceof AdminGitHubError) {
          setError(`Couldn’t delete from GitHub: ${err.message}`)
        } else if (err instanceof AdminWritesDisabledError) {
          setError('This server is read-only (local content source).')
        } else if (err instanceof AdminWarmingError) {
          setError('The server is still warming up; try again in a moment.')
        } else if (!(err instanceof AdminUnauthorizedError)) {
          setError(err instanceof Error ? err.message : 'Delete failed')
        }
      } finally {
        setBusySlug(null)
      }
    },
    [writesEnabled, load],
  )

  return (
    <section className="mt-10 border-t border-line pt-6">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-lg font-semibold">Posts in this page</h3>
        <button
          type="button"
          className={btnPrimary}
          disabled={!writesEnabled}
          onClick={() => navigate(`/admin/posts/new?page=${encodeURIComponent(pageSlug)}`)}
        >
          Create post
        </button>
      </div>

      {error && (
        <p className={`mb-4 ${errorBox}`} role="alert">
          {error}
        </p>
      )}

      {status === 'loading' && <p className="text-muted">Loading posts…</p>}
      {status === 'warming' && (
        <p className="text-muted">
          The content cache is warming up.{' '}
          <button type="button" className={linkButton} onClick={() => load()}>
            Retry
          </button>
        </p>
      )}
      {status === 'error' && (
        <p className="text-muted">
          Couldn’t load posts.{' '}
          <button type="button" className={linkButton} onClick={() => load()}>
            Retry
          </button>
        </p>
      )}

      {status === 'ready' && posts && posts.length === 0 && (
        <p className="text-muted">No posts filed under this page yet.</p>
      )}

      {status === 'ready' && posts && posts.length > 0 && (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th scope="col" className={th}>
                  Title
                </th>
                <th scope="col" className={th}>
                  Status
                </th>
                <th scope="col" className={th}>
                  Date
                </th>
                <th scope="col" className={`${th} text-right`}>
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {posts.map((post) => {
                const confirming = confirmSlug === post.slug
                const deleting = busySlug === post.slug
                return (
                  <tr key={post.slug} className={rowHover}>
                    <td className={td}>
                      <span className="break-words font-semibold text-text">
                        {post.title}
                      </span>
                    </td>
                    <td className={td}>
                      <span className={post.published ? badgePublished : badgeDraft}>
                        {post.published ? 'Published' : 'Draft'}
                      </span>
                    </td>
                    <td className={`${td} whitespace-nowrap text-muted`}>
                      {formatDate(post.published_at)}
                    </td>
                    <td className={`${td} whitespace-nowrap text-right`}>
                      {confirming ? (
                        <span className="inline-flex items-center gap-2">
                          <span className="text-sm text-muted">Delete?</span>
                          <button
                            type="button"
                            className={btnPrimary}
                            disabled={!writesEnabled || deleting}
                            onClick={() => void onDelete(post)}
                          >
                            {deleting ? 'Deleting…' : 'Confirm'}
                          </button>
                          <button
                            type="button"
                            className={btn}
                            disabled={deleting}
                            onClick={() => setConfirmSlug(null)}
                          >
                            Cancel
                          </button>
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-2">
                          {writesEnabled ? (
                            <Link
                              className={btn}
                              to={`/admin/posts/${encodeURIComponent(post.slug)}/edit`}
                            >
                              Edit
                            </Link>
                          ) : (
                            <span className={btn} aria-disabled="true">
                              Edit
                            </span>
                          )}
                          <button
                            type="button"
                            className={btn}
                            disabled={!writesEnabled}
                            onClick={() => {
                              setError('')
                              setConfirmSlug(post.slug)
                            }}
                          >
                            Delete
                          </button>
                        </span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
