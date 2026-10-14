// Public page view for /:pageSlug (R4). Fetches GET /api/pages/{slug} and
// renders the page title + its sanitized prose HTML (reusing the Post page's
// prose/post-content classes and reading measure), then the page's published
// posts via the shared PostCard.
//
// SAFETY: `page.html` is injected with dangerouslySetInnerHTML. This mirrors
// Post.tsx and is safe: the backend renders the Editor.js document to HTML and
// sanitizes it with bluemonday before storing it (see internal/content), so the
// HTML returned by /api/pages/{slug} is already trusted. We do NOT re-sanitize.
//
// An unknown, draft, or reserved slug renders the shared NotFound component
// INLINE (no redirect) so the URL stays /<slug>; the Go server independently
// sets the 404 document status for that path (internal/server/meta.go).
//
// Chunk discipline: this imports only visitor-bundle modules (PostCard, Layout
// via the route tree, query hooks, shared states) — never admin or Editor.js.

import { useEffect } from 'react'

import { useParams } from 'react-router-dom'

import { ApiError, WarmingError } from '../lib/api'
import { usePage } from '../lib/queries'
import { isReservedSlug } from '../lib/reserved'
import PostCard from '../components/PostCard'
import { ErrorState, Warming } from '../components/QueryStates'
import { PostBodySkeleton } from '../components/Skeletons'
import NotFound from './NotFound'

export default function Page() {
  const { pageSlug = '' } = useParams()

  // Client reserved-slug guard (defense in depth; the server also 404s). A
  // reserved slug never fetches — render the shared NotFound inline.
  const reserved = isReservedSlug(pageSlug)
  const query = usePage(reserved ? '' : pageSlug)

  const title = query.data?.page.title

  useEffect(() => {
    if (title) document.title = title
    return () => {
      document.title = 'Himanshu Raj Aman'
    }
  }, [title])

  if (reserved) return <NotFound />

  if (query.isPending) {
    if (query.failureReason instanceof WarmingError) return <Warming />
    return <PostBodySkeleton />
  }

  if (query.isError) {
    // Unknown or draft slug -> 404 from the API -> the shared NotFound inline.
    if (query.error instanceof ApiError && query.error.status === 404) {
      return <NotFound />
    }
    return <ErrorState message={query.error.message} />
  }

  const { page, posts } = query.data

  return (
    <article>
      <header className="mb-8">
        <h1 className="text-4xl font-bold leading-tight tracking-tight sm:text-[2.5rem]">
          {page.title}
        </h1>
      </header>

      {/* Pre-sanitized server HTML (bluemonday). See the SAFETY note above.
          Same classes + reading measure as Post.tsx's body. */}
      <div
        className="prose post-content max-w-[72ch]"
        dangerouslySetInnerHTML={{ __html: page.html }}
      />

      {posts.length > 0 && (
        <div className="mt-10 grid gap-5 lg:grid-cols-2">
          {posts.map((post) => (
            <PostCard key={post.slug} post={post} />
          ))}
        </div>
      )}
    </article>
  )
}
