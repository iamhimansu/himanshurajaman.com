// Reader table of contents for a post. Fed the h2/h3 headings parsed from the
// rendered article DOM (see Post.tsx). Renders two variants from the same list:
//   - variant="rail": a sticky side rail shown on wide (lg+) viewports.
//   - variant="disclosure": a collapsible <details> for narrow viewports.
// The active id (driven by Post.tsx's scroll-spy IntersectionObserver) gets the
// accent colour + an accent left border. Clicking an entry jumps to the heading
// (respecting prefers-reduced-motion) and optimistically sets it active.

import { useCallback } from 'react'

export interface TocHeading {
  id: string
  text: string
  level: number
}

interface PostTocProps {
  headings: TocHeading[]
  activeId: string | null
  onSelect: (id: string) => void
  variant: 'rail' | 'disclosure'
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

function TocList({
  headings,
  activeId,
  onSelect,
}: Omit<PostTocProps, 'variant'>) {
  const handleClick = useCallback(
    (event: React.MouseEvent<HTMLAnchorElement>, id: string) => {
      const el = document.getElementById(id)
      if (!el) return
      event.preventDefault()
      el.scrollIntoView({
        behavior: prefersReducedMotion() ? 'auto' : 'smooth',
        block: 'start',
      })
      if (typeof history.replaceState === 'function') {
        history.replaceState(null, '', `#${id}`)
      }
      onSelect(id)
    },
    [onSelect],
  )

  return (
    <ul className="flex flex-col gap-0.5 text-sm">
      {headings.map((h) => {
        const active = h.id === activeId
        return (
          <li key={h.id}>
            <a
              href={`#${h.id}`}
              onClick={(event) => handleClick(event, h.id)}
              aria-current={active ? 'location' : undefined}
              className={`block border-l-2 py-1.5 ${
                h.level >= 3 ? 'pl-7' : 'pl-4'
              } ${
                active
                  ? 'border-accent font-medium text-accent'
                  : 'border-line text-muted hover:text-text'
              }`}
            >
              {h.text}
            </a>
          </li>
        )
      })}
    </ul>
  )
}

const HEADING_LABEL =
  'text-[13px] font-semibold uppercase tracking-[0.08em] text-muted'

export default function PostToc({
  headings,
  activeId,
  onSelect,
  variant,
}: PostTocProps) {
  if (headings.length === 0) return null

  if (variant === 'rail') {
    return (
      <nav
        aria-label="On this page"
        className="sticky top-10 hidden max-h-[calc(100vh-5rem)] overflow-auto lg:block"
      >
        <p className={`mb-4 ${HEADING_LABEL}`}>On this page</p>
        <TocList headings={headings} activeId={activeId} onSelect={onSelect} />
      </nav>
    )
  }

  return (
    <details className="mb-6 border border-line lg:hidden">
      <summary className={`cursor-pointer px-4 py-2 ${HEADING_LABEL}`}>
        On this page
      </summary>
      <nav aria-label="On this page" className="border-t border-line px-4 py-3">
        <TocList headings={headings} activeId={activeId} onSelect={onSelect} />
      </nav>
    </details>
  )
}
