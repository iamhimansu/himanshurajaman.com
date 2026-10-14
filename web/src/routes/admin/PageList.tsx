// Page list (drafts included). Reads GET /api/admin/pages and renders every
// page as a row in a sharp bordered TABLE/GRIDVIEW: Title, Nav (yes/no), Slug,
// Status badge, Post count and Actions (Edit + an inline Delete confirm). A
// "New page" button sits above the table. Delete goes through adminApi (the
// server uses the cached blob sha when none is supplied); every typed error
// state is surfaced and read-only mode disables New/Edit/Delete. After a
// successful delete the list is reloaded from the server. Mirrors PostList.tsx.

import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  AdminWritesDisabledError,
  deletePage,
  listPages,
} from '../../lib/adminApi'
import type { AdminPageSummary } from '../../lib/types'
import { PostListSkeleton } from '../../components/Skeletons'
import { useAdminAuth } from './AdminApp'
import {
  badgeDraft,
  badgePublished,
  banner,
  btn,
  btnPrimary,
  errorBox,
  linkButton,
  rowHover,
  table,
  td,
  th,
} from './ui'

export default function PageList() {
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const [pages, setPages] = useState<AdminPageSummary[] | null>(null)
  const [status, setStatus] = useState<'loading' | 'warming' | 'ready' | 'error'>('loading')
  const [error, setError] = useState<string>('')
  const [busySlug, setBusySlug] = useState<string | null>(null)
  // The slug whose row is showing the inline "Delete? Confirm / Cancel" prompt.
  const [confirmSlug, setConfirmSlug] = useState<string | null>(null)

  useEffect(() => {
    document.title = 'Pages · Admin'
  }, [])

  const load = useCallback((signal?: AbortSignal) => {
    setStatus('loading')
    listPages(signal)
      .then((items) => {
        if (signal?.aborted) return
        setPages(items)
        setStatus('ready')
      })
      .catch((err) => {
        if (signal?.aborted) return
        if (err instanceof AdminUnauthorizedError) return // guard redirects
        if (err instanceof AdminWarmingError) {
          setStatus('warming')
          return
        }
        setError(err instanceof Error ? err.message : 'Failed to load pages')
        setStatus('error')
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  const onDelete = useCallback(
    async (page: AdminPageSummary) => {
      if (!writesEnabled) return
      setBusySlug(page.slug)
      setError('')
      try {
        await deletePage(page.slug)
        setConfirmSlug(null)
        // Refresh from the server rather than trusting local state.
        load()
      } catch (err) {
        if (err instanceof AdminStaleError) {
          setError('This page changed on the server; refresh the list and try again.')
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
    <section>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
        <h2 className="text-xl font-semibold">Pages</h2>
        <button
          type="button"
          className={btnPrimary}
          disabled={!writesEnabled}
          onClick={() => navigate('/admin/pages/new')}
        >
          New page
        </button>
      </div>

      {!writesEnabled && (
        <p className={`mb-4 ${banner}`} role="status">
          This server is read-only (local content source). Editing is disabled.
        </p>
      )}

      {error && (
        <p className={`mb-4 ${errorBox}`} role="alert">
          {error}
        </p>
      )}

      {status === 'loading' && <PostListSkeleton count={4} />}
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
          Couldn’t load pages.{' '}
          <button type="button" className={linkButton} onClick={() => load()}>
            Retry
          </button>
        </p>
      )}

      {status === 'ready' && pages && pages.length === 0 && (
        <p className="text-muted">No pages yet.</p>
      )}

      {status === 'ready' && pages && pages.length > 0 && (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th scope="col" className={th}>
                  Title
                </th>
                <th scope="col" className={th}>
                  Nav
                </th>
                <th scope="col" className={th}>
                  Slug
                </th>
                <th scope="col" className={th}>
                  Status
                </th>
                <th scope="col" className={th}>
                  Posts
                </th>
                <th scope="col" className={`${th} text-right`}>
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {pages.map((page) => {
                const confirming = confirmSlug === page.slug
                const deleting = busySlug === page.slug
                return (
                  <tr key={page.slug} className={rowHover}>
                    <td className={td}>
                      <span className="break-words font-semibold text-text">
                        {page.title}
                      </span>
                    </td>
                    <td className={`${td} whitespace-nowrap text-muted`}>
                      {page.nav ? 'Yes' : 'No'}
                    </td>
                    <td className={`${td} whitespace-nowrap`}>
                      <span className="font-mono text-xs text-muted">{page.slug}</span>
                    </td>
                    <td className={td}>
                      <span className={page.published ? badgePublished : badgeDraft}>
                        {page.published ? 'Published' : 'Draft'}
                      </span>
                    </td>
                    <td className={`${td} whitespace-nowrap text-muted`}>
                      {page.post_count}
                    </td>
                    <td className={`${td} whitespace-nowrap text-right`}>
                      {confirming ? (
                        <span className="inline-flex items-center gap-2">
                          <span className="text-sm text-muted">Delete?</span>
                          <button
                            type="button"
                            className={btnPrimary}
                            disabled={!writesEnabled || deleting}
                            onClick={() => void onDelete(page)}
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
                              to={`/admin/pages/${encodeURIComponent(page.slug)}/edit`}
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
                              setConfirmSlug(page.slug)
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
