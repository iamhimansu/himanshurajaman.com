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
    <article className="border border-line p-4">
      {cover && (
        <Link
          to={`/blog/${post.slug}`}
          className="mb-3 block"
          tabIndex={-1}
          aria-hidden="true"
        >
          <img
            src={cover}
            alt=""
            className="block aspect-[2/1] w-full border border-line object-cover"
            width={1200}
            height={600}
            loading="lazy"
            decoding="async"
          />
        </Link>
      )}
      <h2 className="text-xl font-bold leading-snug tracking-tight">
        <Link to={`/blog/${post.slug}`} className="hover:text-accent">
          {post.title}
        </Link>
      </h2>
      <p className="mt-1.5 text-sm text-muted">
        <time dateTime={post.published_at}>{formatDate(post.published_at)}</time>
        <span aria-hidden="true"> · </span>
        <span>{readingTime(post.reading_minutes)}</span>
      </p>
      {post.tags.length > 0 && (
        <ul className="mt-3 flex flex-wrap gap-2" aria-label="Tags">
          {post.tags.map((tag) => (
            <li key={tag}>
              <Link
                to={`/blog?tag=${encodeURIComponent(slugify(tag))}`}
                className="border border-line px-2 py-0.5 font-mono text-xs text-muted hover:border-line-strong hover:text-text"
              >
                {tag}
              </Link>
            </li>
          ))}
        </ul>
      )}
      {post.summary && <p className="mt-3 text-base text-muted">{post.summary}</p>}
      <p className="mt-4">
        <Link
          to={`/blog/${post.slug}`}
          aria-label={`Read ${post.title}`}
          className="group inline-flex items-center gap-1 text-sm font-semibold text-accent hover:underline"
        >
          Read
          <span
            aria-hidden="true"
            className="transition-transform group-hover:translate-x-0.5"
          >
            →
          </span>
        </Link>
      </p>
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
