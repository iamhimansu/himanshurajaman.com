// Single post. The `html` field is injected with dangerouslySetInnerHTML.
//
// SAFETY: this is intentional and safe. The backend renders the Editor.js
// document to HTML on the server and then sanitizes the result with bluemonday
// (see internal/content/editorjs.go) before storing it, so the HTML returned by
// /api/posts/{slug} is already a trusted, sanitized subset. We do NOT sanitize
// again on the client. Syntax highlighting is CSS-class based (styles/code.css).

import { useEffect } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ApiError, imageUrl } from '../lib/api'
import { formatDate, readingTime } from '../lib/format'
import { usePost } from '../lib/queries'
import { ErrorState, PendingState } from '../components/QueryStates'

export default function Post() {
  const { slug = '' } = useParams()
  const query = usePost(slug)

  useEffect(() => {
    const title = query.data?.title
    if (title) document.title = title
    return () => {
      document.title = 'Himanshu Raj Aman'
    }
  }, [query.data?.title])

  if (query.isPending) {
    return <PendingState failureReason={query.failureReason} />
  }

  if (query.isError) {
    // Unknown or draft slug -> 404 from the API -> not-found state.
    if (query.error instanceof ApiError && query.error.status === 404) {
      return <NotFoundPost />
    }
    return <ErrorState message={query.error.message} />
  }

  const post = query.data
  const cover = imageUrl(post.cover_image)

  return (
    <article className="post">
      <header className="post-header">
        <h1>{post.title}</h1>
        <p className="post-meta muted">
          <time dateTime={post.published_at}>{formatDate(post.published_at)}</time>
          {post.updated_at && (
            <>
              <span aria-hidden="true"> · </span>
              <span>Updated {formatDate(post.updated_at)}</span>
            </>
          )}
          <span aria-hidden="true"> · </span>
          <span>{readingTime(post.reading_minutes)}</span>
        </p>
        {post.tags.length > 0 && (
          <ul className="tag-list" aria-label="Tags">
            {post.tags.map((tag) => (
              <li key={tag}>
                <span className="tag">{tag}</span>
              </li>
            ))}
          </ul>
        )}
      </header>

      {cover && (
        <img
          src={cover}
          alt={post.title}
          className="cover post-cover"
          width={1200}
          height={600}
          loading="lazy"
          decoding="async"
        />
      )}

      {/* Pre-sanitized server HTML (bluemonday). See the SAFETY note above. */}
      <div className="post-content" dangerouslySetInnerHTML={{ __html: post.html }} />

      <hr className="divider" />
      <p>
        <Link to="/blog">← Back to blog</Link>
      </p>
    </article>
  )
}

function NotFoundPost() {
  return (
    <div className="state">
      <h1>Post not found</h1>
      <p className="muted">
        This post doesn’t exist or hasn’t been published.
      </p>
      <p>
        <Link to="/blog">← Back to blog</Link>
      </p>
    </div>
  )
}
