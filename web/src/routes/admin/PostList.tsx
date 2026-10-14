// Post list (drafts included). Reads GET /api/admin/posts and renders every
// post as a row in a sharp bordered TABLE/GRIDVIEW: Title, Status badge, Date,
// Tags and Actions (Edit + an inline Delete confirm). A "New post" button sits
// above the table. Delete goes through adminApi (the server uses the cached
// blob sha when none is supplied); every typed error state is surfaced and
// read-only mode disables New/Edit/Delete. After a successful delete the list
// is reloaded from the server.

import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  AdminWritesDisabledError,
  deletePost,
  listPosts,
} from '../../lib/adminApi'
import type { AdminPostSummary } from '../../lib/types'
import { formatDate } from '../../lib/format'
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
  tagChip,
  td,
  th,
} from './ui'

export default function PostList() {
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const [posts, setPosts] = useState<AdminPostSummary[] | null>(null)
  const [status, setStatus] = useState<'loading' | 'warming' | 'ready' | 'error'>('loading')
  const [error, setError] = useState<string>('')
  const [busySlug, setBusySlug] = useState<string | null>(null)
  // The slug whose row is showing the inline "Delete? Confirm / Cancel" prompt.
  const [confirmSlug, setConfirmSlug] = useState<string | null>(null)

  useEffect(() => {
    document.title = 'Posts · Admin'
  }, [])

  const load = useCallback((signal?: AbortSignal) => {
    setStatus('loading')
    listPosts('all', undefined, signal)
      .then((items) => {
        if (signal?.aborted) return
        setPosts(items)
        setStatus('ready')
      })
      .catch((err) => {
        if (signal?.aborted) return
        if (err instanceof AdminUnauthorizedError) return // guard redirects
        if (err instanceof AdminWarmingError) {
          setStatus('warming')
          return
        }
        setError(err instanceof Error ? err.message : 'Failed to load posts')
        setStatus('error')
      })
  }, [])

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
        // Refresh from the server rather than trusting local state.
        load()
      } catch (err) {
        if (err instanceof AdminStaleError) {
          setError('This post changed on the server; refresh the list and try again.')
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
        <h2 className="text-xl font-semibold">Posts</h2>
        <button
          type="button"
          className={btnPrimary}
          disabled={!writesEnabled}
          onClick={() => navigate('/admin/posts/new')}
        >
          New post
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
          Couldn’t load posts.{' '}
          <button type="button" className={linkButton} onClick={() => load()}>
            Retry
          </button>
        </p>
      )}

      {status === 'ready' && posts && posts.length === 0 && (
        <p className="text-muted">No posts yet.</p>
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
                <th scope="col" className={th}>
                  Tags
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
                    <td className={td}>
                      {post.tags.length > 0 ? (
                        <span className="flex flex-wrap gap-1.5">
                          {post.tags.map((tag) => (
                            <span className={tagChip} key={tag}>
                              {tag}
                            </span>
                          ))}
                        </span>
                      ) : (
                        <span className="text-muted">—</span>
                      )}
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
