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

  const pill = (active: boolean) =>
    `border px-2.5 py-0.5 text-[13px] ${
      active
        ? 'border-text bg-text text-white'
        : 'border-line text-muted hover:border-line-strong'
    }`

  return (
    <div>
      <header className="mb-2 pt-2">
        <h1 className="text-3xl font-bold tracking-tight">Blog</h1>
        <p className="mt-1.5 text-[15px] text-muted">
          Notes on bugs, research, and benchmarks.
        </p>
      </header>

      {tags.data && tags.data.items.length > 0 && (
        <nav className="mb-5 mt-4 flex flex-wrap gap-2" aria-label="Filter by tag">
          <Link to="/blog" className={pill(!tag)}>
            All
          </Link>
          {tags.data.items.map((t) => (
            <Link
              key={t.slug}
              to={`/blog?tag=${encodeURIComponent(t.slug)}`}
              className={pill(t.slug === tag)}
            >
              {t.name} ({t.count})
            </Link>
          ))}
        </nav>
      )}

      {tag && (
        <p className="mb-4 text-[15px] text-muted">
          Showing posts tagged <strong>{activeTagName}</strong>.{' '}
          <Link to="/blog" className="text-accent hover:underline">
            Clear
          </Link>
        </p>
      )}

      <PostResults
        query={query}
        emptyMessage={tag ? 'No posts with this tag yet.' : 'No posts yet.'}
      />
    </div>
  )
}
