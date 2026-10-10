// A single post summary card, reused by the blog list and search results.
// The cover image (when present) is lazy-loaded with explicit width/height so
// the browser reserves space and the layout doesn't shift when it loads.

import { Link } from 'react-router-dom'

import { imageUrl } from '../lib/api'
import { formatDate, readingTime } from '../lib/format'
import type { PostSummary } from '../lib/types'

export default function PostCard({ post }: { post: PostSummary }) {
  const cover = imageUrl(post.cover_image)
  return (
    <article className="post-card">
      {cover && (
        <Link to={`/blog/${post.slug}`} className="post-card-cover" tabIndex={-1} aria-hidden="true">
          <img
            src={cover}
            alt=""
            className="cover"
            width={1200}
            height={600}
            loading="lazy"
            decoding="async"
          />
        </Link>
      )}
      <h2 className="post-card-title">
        <Link to={`/blog/${post.slug}`}>{post.title}</Link>
      </h2>
      <p className="post-meta muted">
        <time dateTime={post.published_at}>{formatDate(post.published_at)}</time>
        <span aria-hidden="true"> · </span>
        <span>{readingTime(post.reading_minutes)}</span>
      </p>
      {post.tags.length > 0 && (
        <ul className="tag-list" aria-label="Tags">
          {post.tags.map((tag) => (
            <li key={tag}>
              <Link to={`/blog?tag=${encodeURIComponent(slugify(tag))}`} className="tag">
                {tag}
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="post-summary">{post.summary}</p>
    </article>
  )
}

// Tag links filter the blog by slug (the backend's tag filter is slug-based).
// This mirrors the server's Slugify for the common ASCII case.
function slugify(name: string): string {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}
