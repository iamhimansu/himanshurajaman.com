// Post list (drafts included). Reads GET /api/admin/posts and renders each as a
// prototype .project row with a published/draft badge and New / Edit / Delete
// actions. Delete goes through adminApi (server uses the cached blob sha when
// none is supplied). Styled with the Part A tokens.

import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWarmingError,
  deletePost,
  listPosts,
} from '../../lib/adminApi'
import type { AdminPostSummary } from '../../lib/types'
import { formatDate } from '../../lib/format'
import { useAdminAuth } from './AdminApp'

export default function PostList() {
  const { writesEnabled, signOut, me } = useAdminAuth()
  const navigate = useNavigate()
  const [posts, setPosts] = useState<AdminPostSummary[] | null>(null)
  const [status, setStatus] = useState<'loading' | 'warming' | 'ready' | 'error'>('loading')
  const [error, setError] = useState<string>('')
  const [busySlug, setBusySlug] = useState<string | null>(null)

  useEffect(() => {
    document.title = 'Posts · Admin'
  }, [])

  const load = useCallback((signal?: AbortSignal) => {
    setStatus('loading')
    listPosts('all', signal)
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
      if (!window.confirm(`Delete “${post.title}”? This removes it from GitHub.`)) return
      setBusySlug(post.slug)
      setError('')
      try {
        await deletePost(post.slug)
        setPosts((prev) => (prev ? prev.filter((p) => p.slug !== post.slug) : prev))
      } catch (err) {
        if (err instanceof AdminStaleError) {
          setError('This post changed on the server; refresh the list and try again.')
        } else if (!(err instanceof AdminUnauthorizedError)) {
          setError(err instanceof Error ? err.message : 'Delete failed')
        }
      } finally {
        setBusySlug(null)
      }
    },
    [writesEnabled],
  )

  return (
    <section className="admin-posts">
      <div className="admin-bar">
        <h2>Posts</h2>
        <div className="admin-bar-actions">
          <Link className="btn" to="/admin/resume">
            Resume
          </Link>
          <button
            type="button"
            className="btn primary"
            disabled={!writesEnabled}
            onClick={() => navigate('/admin/posts/new')}
          >
            New post
          </button>
          <button type="button" className="btn" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      </div>

      {me && <p className="muted admin-whoami">Signed in as {me.email}</p>}

      {!writesEnabled && (
        <p className="admin-banner" role="status">
          This server is read-only (local content source). Editing is disabled.
        </p>
      )}

      {error && (
        <p className="admin-error" role="alert">
          {error}
        </p>
      )}

      {status === 'loading' && <p className="muted">Loading posts…</p>}
      {status === 'warming' && (
        <p className="muted">
          The content cache is warming up.{' '}
          <button type="button" className="link-button" onClick={() => load()}>
            Retry
          </button>
        </p>
      )}
      {status === 'error' && (
        <p className="muted">
          Couldn’t load posts.{' '}
          <button type="button" className="link-button" onClick={() => load()}>
            Retry
          </button>
        </p>
      )}

      {status === 'ready' && posts && posts.length === 0 && (
        <p className="muted">No posts yet.</p>
      )}

      {status === 'ready' &&
        posts &&
        posts.map((post) => (
          <div className="project" key={post.slug}>
            <div className="row">
              <h3>{post.title}</h3>
              <span className={post.published ? 'badge badge-published' : 'badge badge-draft'}>
                {post.published ? 'Published' : 'Draft'}
              </span>
            </div>
            <p>
              {formatDate(post.published_at)}
              {post.summary ? ` · ${post.summary}` : ''}
            </p>
            <div className="admin-row-actions">
              <Link className="btn" to={`/admin/posts/${encodeURIComponent(post.slug)}/edit`}>
                Edit
              </Link>
              <button
                type="button"
                className="btn"
                disabled={!writesEnabled || busySlug === post.slug}
                onClick={() => void onDelete(post)}
              >
                {busySlug === post.slug ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        ))}
    </section>
  )
}
