// A thin fixed reading-progress bar pinned to the top of the viewport. Its fill
// width tracks how far the referenced article element has been scrolled through.
// Updates are coalesced via requestAnimationFrame on a passive scroll listener,
// and all listeners are removed on unmount. The fill transition is disabled
// under prefers-reduced-motion (motion-reduce:transition-none).

import { useEffect, useRef, useState } from 'react'

interface ReadingProgressProps {
  targetRef: React.RefObject<HTMLElement | null>
}

export default function ReadingProgress({ targetRef }: ReadingProgressProps) {
  const [progress, setProgress] = useState(0)
  const ticking = useRef(false)

  useEffect(() => {
    const compute = () => {
      ticking.current = false
      const el = targetRef.current
      if (!el) return
      const top = el.getBoundingClientRect().top + window.scrollY
      const total = el.offsetHeight - window.innerHeight
      if (total <= 0) {
        setProgress(window.scrollY >= top ? 1 : 0)
        return
      }
      const scrolled = window.scrollY - top
      setProgress(Math.min(1, Math.max(0, scrolled / total)))
    }

    const onScroll = () => {
      if (ticking.current) return
      ticking.current = true
      window.requestAnimationFrame(compute)
    }

    compute()
    window.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('resize', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      window.removeEventListener('resize', onScroll)
    }
  }, [targetRef])

  return (
    <div
      className="fixed inset-x-0 top-0 z-50 h-1 bg-line"
      role="progressbar"
      aria-label="Reading progress"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(progress * 100)}
    >
      <div
        className="h-full bg-accent transition-[width] duration-150 ease-out motion-reduce:transition-none"
        style={{ width: `${progress * 100}%` }}
      />
    </div>
  )
}
