// Renders an infinite post query (blog list or search results): pending/warming
// state, error state, the cards, and the Load more button. Shared so blog and
// search behave identically.

import type { UseInfiniteQueryResult, InfiniteData } from '@tanstack/react-query'

import type { PostList } from '../lib/types'
import LoadMore from './LoadMore'
import PostCard from './PostCard'
import { ErrorState, PendingState } from './QueryStates'

type Query = UseInfiniteQueryResult<InfiniteData<PostList>, Error>

export default function PostResults({
  query,
  emptyMessage,
}: {
  query: Query
  emptyMessage: string
}) {
  if (query.isPending) {
    return <PendingState failureReason={query.failureReason} />
  }
  if (query.isError) {
    return <ErrorState message={query.error.message} />
  }

  const posts = query.data.pages.flatMap((page) => page.items)
  if (posts.length === 0) {
    return <p className="muted">{emptyMessage}</p>
  }

  return (
    <>
      <div className="post-list">
        {posts.map((post) => (
          <PostCard key={post.slug} post={post} />
        ))}
      </div>
      <LoadMore
        hasNextPage={query.hasNextPage}
        isFetchingNextPage={query.isFetchingNextPage}
        onClick={() => void query.fetchNextPage()}
      />
    </>
  )
}
