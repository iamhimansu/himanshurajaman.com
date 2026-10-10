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
    <div className="load-more">
      <button
        type="button"
        className="btn"
        onClick={onClick}
        disabled={isFetchingNextPage}
      >
        {isFetchingNextPage ? 'Loading…' : 'Load more'}
      </button>
    </div>
  )
}
