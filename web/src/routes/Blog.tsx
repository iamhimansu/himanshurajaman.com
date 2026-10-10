// Blog list: published posts newest-first (the backend excludes drafts).
// ?tag=<slug> filters by tag; cursor pagination via a "Load more" button.

import { useEffect } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { usePosts, useTags } from '../lib/queries'
import PostResults from '../components/PostResults'

export default function Blog() {
  const [params] = useSearchParams()
  const tag = params.get('tag') ?? ''
  const query = usePosts(tag || undefined)
  const tags = useTags()

  useEffect(() => {
    document.title = tag ? `Blog · #${tag}` : 'Blog'
  }, [tag])

  const activeTagName =
    tags.data?.items.find((t) => t.slug === tag)?.name ?? tag

  return (
    <div className="blog">
      <header className="page-header">
        <h1>Blog</h1>
        <p className="muted">Notes on bugs, research, and benchmarks.</p>
      </header>

      {tags.data && tags.data.items.length > 0 && (
        <nav className="tag-filter" aria-label="Filter by tag">
          <Link to="/blog" className={tag ? undefined : 'active'}>
            All
          </Link>
          {tags.data.items.map((t) => (
            <Link
              key={t.slug}
              to={`/blog?tag=${encodeURIComponent(t.slug)}`}
              className={t.slug === tag ? 'active' : undefined}
            >
              {t.name} ({t.count})
            </Link>
          ))}
        </nav>
      )}

      {tag && (
        <p className="muted filter-note">
          Showing posts tagged <strong>{activeTagName}</strong>.{' '}
          <Link to="/blog">Clear</Link>
        </p>
      )}

      <PostResults
        query={query}
        emptyMessage={tag ? 'No posts with this tag yet.' : 'No posts yet.'}
      />
    </div>
  )
}
