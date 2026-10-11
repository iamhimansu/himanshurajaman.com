// Single post. The `html` field is injected with dangerouslySetInnerHTML.
//
// SAFETY: this is intentional and safe. The backend renders the Editor.js
// document to HTML on the server and then sanitizes the result with bluemonday
// (see internal/content/editorjs.go) before storing it, so the HTML returned by
// /api/posts/{slug} is already a trusted, sanitized subset. We do NOT sanitize
// again on the client. Syntax highlighting is CSS-class based (styles/code.css).

import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ApiError, WarmingError, imageUrl } from '../lib/api'
import { formatDate, readingTime } from '../lib/format'
import { usePost } from '../lib/queries'
import { ErrorState, Warming } from '../components/QueryStates'
import { PostBodySkeleton } from '../components/Skeletons'
import PostToc, { type TocHeading } from '../components/PostToc'
import ReadingProgress from '../components/ReadingProgress'
import ListenButton from '../components/ListenButton'

export default function Post() {
  const { slug = '' } = useParams()
  const query = usePost(slug)

  const contentRef = useRef<HTMLDivElement>(null)
  const [headings, setHeadings] = useState<TocHeading[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)

  const html = query.data?.html

  useEffect(() => {
    const title = query.data?.title
    if (title) document.title = title
    return () => {
      document.title = 'Himanshu Raj Aman'
    }
  }, [query.data?.title])

  // Parse the server-rendered article's h2/h3 anchors into TOC entries. Runs
  // after each html change (the content div is only mounted on success).
  useEffect(() => {
    const container = contentRef.current
    if (!container) {
      setHeadings([])
      setActiveId(null)
      return
    }
    const nodes = Array.from(
      container.querySelectorAll<HTMLElement>('h2[id], h3[id]'),
    )
    const parsed: TocHeading[] = nodes.map((node) => {
      // Give headings breathing room so click-to-jump doesn't pin them to the
      // very top edge. Inline style, not a utility class on the server HTML.
      node.style.scrollMarginTop = '5rem'
      return {
        id: node.id,
        text: node.textContent?.trim() ?? '',
        level: node.tagName === 'H3' ? 3 : 2,
      }
    })
    setHeadings(parsed)
    setActiveId(parsed.length > 0 ? parsed[0].id : null)
  }, [html])

  // Scroll-spy: a SINGLE IntersectionObserver over the heading elements. A
  // heading becomes active when it enters the band near the top of the
  // viewport; we pick the topmost one currently in that band. Disconnected on
  // unmount and whenever the heading set changes.
  useEffect(() => {
    if (headings.length === 0) return
    const els = headings
      .map((h) => document.getElementById(h.id))
      .filter((el): el is HTMLElement => el !== null)
    if (els.length === 0) return

    const visible = new Set<string>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id)
          else visible.delete(entry.target.id)
        }
        const topmost = headings.find((h) => visible.has(h.id))
        if (topmost) setActiveId(topmost.id)
      },
      { rootMargin: '-80px 0px -70% 0px', threshold: 0 },
    )
    els.forEach((el) => observer.observe(el))
    return () => observer.disconnect()
  }, [headings])

  if (query.isPending) {
    if (query.failureReason instanceof WarmingError) return <Warming />
    return <PostBodySkeleton />
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
    <article>
      <ReadingProgress targetRef={contentRef} />
      <header className="mb-8">
        <h1 className="text-4xl font-bold leading-tight tracking-tight sm:text-[2.5rem]">
          {post.title}
        </h1>
        <p className="mt-2 text-sm text-muted">
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
          <ul className="mt-3 flex flex-wrap gap-2" aria-label="Tags">
            {post.tags.map((tag) => (
              <li key={tag}>
                <span className="border border-line px-2 py-0.5 font-mono text-xs text-muted">
                  {tag}
                </span>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-4">
          <ListenButton targetRef={contentRef} />
        </div>
      </header>

      {cover && (
        <img
          src={cover}
          alt={post.title}
          className="mb-6 block aspect-[2/1] w-full border border-line object-cover"
          width={1200}
          height={600}
          loading="lazy"
          decoding="async"
        />
      )}

      {/* Collapsible TOC for narrow viewports; hidden on lg+ where the rail
          takes over. Renders nothing when there are no headings. */}
      <PostToc
        variant="disclosure"
        headings={headings}
        activeId={activeId}
        onSelect={setActiveId}
      />

      <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_15rem] lg:gap-12">
        {/* Pre-sanitized server HTML (bluemonday). See the SAFETY note above.
            `prose` styles the body via the typography plugin; `post-content`
            keeps code.css chroma styling. No utility classes go on the server
            HTML itself. `max-w-[72ch]` keeps the body at a comfortable reading
            measure instead of filling the full column edge-to-edge. */}
        <div
          ref={contentRef}
          className="prose post-content max-w-[72ch]"
          dangerouslySetInnerHTML={{ __html: post.html }}
        />

        {headings.length > 0 && (
          <aside className="hidden lg:block">
            <PostToc
              variant="rail"
              headings={headings}
              activeId={activeId}
              onSelect={setActiveId}
            />
          </aside>
        )}
      </div>

      <hr className="my-10 border-0 border-t border-line" />
      <p>
        <Link to="/blog" className="text-accent hover:underline">
          ← Back to blog
        </Link>
      </p>
    </article>
  )
}

function NotFoundPost() {
  return (
    <div className="py-6">
      <h1 className="text-[28px] font-semibold leading-tight tracking-tight">
        Post not found
      </h1>
      <p className="mt-1.5 text-muted">
        This post doesn’t exist or hasn’t been published.
      </p>
      <p className="mt-3">
        <Link to="/blog" className="text-accent hover:underline">
          ← Back to blog
        </Link>
      </p>
    </div>
  )
}
