// Plain "Load more" button for cursor pagination (no infinite scroll).

export default function LoadMore({
  hasNextPage,
  isFetchingNextPage,
  onClick,
}: {
  hasNextPage: boolean
  isFetchingNextPage: boolean
  onClick: () => void
}) {
  if (!hasNextPage) return null
  return (
    <div className="mt-6 text-center">
      <button
        type="button"
        className="border border-line bg-surface px-4 py-2 text-sm text-text hover:border-[#a8a29e] disabled:cursor-default disabled:opacity-60"
        onClick={onClick}
        disabled={isFetchingNextPage}
      >
        {isFetchingNextPage ? 'Loading…' : 'Load more'}
      </button>
    </div>
  )
}
