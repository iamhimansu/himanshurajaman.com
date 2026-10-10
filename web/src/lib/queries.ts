// TanStack Query hooks. Retry/stale behaviour is configured globally in
// main.tsx; here we only declare keys and fetchers. Cursor pagination uses
// useInfiniteQuery so "Load more" is a one-liner (fetchNextPage).

import {
  useInfiniteQuery,
  useQuery,
  type UseInfiniteQueryResult,
  type InfiniteData,
} from '@tanstack/react-query'

import { apiGet, qs } from './api'
import type {
  PostDetail,
  PostList,
  ResumeResponse,
  SiteSettings,
  TagsResponse,
} from './types'

export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: ({ signal }) => apiGet<SiteSettings>('/api/settings', signal),
    staleTime: 5 * 60 * 1000,
  })
}

export function useResume() {
  return useQuery({
    queryKey: ['resume'],
    queryFn: ({ signal }) => apiGet<ResumeResponse>('/api/resume', signal),
    staleTime: 5 * 60 * 1000,
  })
}

export function useTags() {
  return useQuery({
    queryKey: ['tags'],
    queryFn: ({ signal }) => apiGet<TagsResponse>('/api/tags', signal),
    staleTime: 5 * 60 * 1000,
  })
}

/** A few most-recent published posts for the home page. */
export function useRecentPosts(limit = 5) {
  return useQuery({
    queryKey: ['recent-posts', limit],
    queryFn: ({ signal }) => apiGet<PostList>(`/api/posts${qs({ limit })}`, signal),
  })
}

export function usePost(slug: string) {
  return useQuery({
    queryKey: ['post', slug],
    queryFn: ({ signal }) =>
      apiGet<PostDetail>(`/api/posts/${encodeURIComponent(slug)}`, signal),
    enabled: slug.length > 0,
  })
}

type PostListInfinite = UseInfiniteQueryResult<InfiniteData<PostList>, Error>

/** Published posts, newest first, optionally filtered by tag slug. */
export function usePosts(tag?: string): PostListInfinite {
  return useInfiniteQuery({
    queryKey: ['posts', { tag: tag ?? '' }],
    queryFn: ({ pageParam, signal }) =>
      apiGet<PostList>(`/api/posts${qs({ tag, cursor: pageParam })}`, signal),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  })
}

/** Full-text search over published posts (empty query is disabled). */
export function useSearch(query: string): PostListInfinite {
  const q = query.trim()
  return useInfiniteQuery({
    queryKey: ['search', q],
    queryFn: ({ pageParam, signal }) =>
      apiGet<PostList>(`/api/search${qs({ q, cursor: pageParam })}`, signal),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: q.length > 0,
  })
}
