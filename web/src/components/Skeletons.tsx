// Loading placeholders. Each uses `animate-pulse` (disabled under
// prefers-reduced-motion via `motion-reduce:animate-none`) and the same 1px
// line-token borders as the real content, so pending states keep the page's
// rectangular rhythm instead of a bare "Loading…" line.

// A single grey bar. `w` lets callers vary the line width.
function Bar({ className = '' }: { className?: string }) {
  return <div className={`h-3 rounded-sm bg-line ${className}`} />
}

export function PostCardSkeleton() {
  return (
    <div
      className="animate-pulse border border-line p-4 motion-reduce:animate-none"
      aria-hidden="true"
    >
      <Bar className="h-4 w-3/4" />
      <Bar className="mt-3 w-1/3" />
      <Bar className="mt-4 w-full" />
      <Bar className="mt-2 w-5/6" />
    </div>
  )
}

export function PostListSkeleton({ count = 3 }: { count?: number }) {
  return (
    <div className="flex flex-col gap-4" role="status" aria-label="Loading posts">
      {Array.from({ length: count }).map((_, i) => (
        <PostCardSkeleton key={i} />
      ))}
    </div>
  )
}

export function PostBodySkeleton() {
  return (
    <div
      className="animate-pulse motion-reduce:animate-none"
      role="status"
      aria-label="Loading post"
    >
      <Bar className="h-7 w-2/3" />
      <Bar className="mt-4 w-1/3" />
      <div className="mt-8 space-y-3">
        <Bar className="w-full" />
        <Bar className="w-full" />
        <Bar className="w-5/6" />
        <Bar className="w-full" />
        <Bar className="w-2/3" />
      </div>
    </div>
  )
}

export function ResumeSkeleton() {
  return (
    <div
      className="animate-pulse motion-reduce:animate-none"
      role="status"
      aria-label="Loading resume"
    >
      <Bar className="h-7 w-1/2" />
      <Bar className="mt-3 w-1/3" />
      <div className="mt-8 flex flex-col gap-6">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="border border-line p-4">
            <Bar className="h-4 w-2/5" />
            <Bar className="mt-3 w-full" />
            <Bar className="mt-2 w-4/5" />
          </div>
        ))}
      </div>
    </div>
  )
}

export function WorksSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div
      className="grid grid-cols-1 gap-4 sm:grid-cols-2"
      role="status"
      aria-label="Loading works"
    >
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="animate-pulse border border-line p-4 motion-reduce:animate-none"
          aria-hidden="true"
        >
          <Bar className="h-4 w-1/2" />
          <Bar className="mt-3 w-1/4" />
          <Bar className="mt-4 w-full" />
          <Bar className="mt-2 w-3/4" />
        </div>
      ))}
    </div>
  )
}
