// Search: query synced to ?q=. The input is debounced into the URL (and the
// query key), so typing stays responsive without a request per keystroke.
// Published-only, ranked results come from /api/search.

import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useSearch } from '../lib/queries'
import PostResults from '../components/PostResults'

export default function Search() {
  const [params, setParams] = useSearchParams()
  const urlQuery = params.get('q') ?? ''

  // Local input state for responsiveness; committed to the URL after a pause.
  const [input, setInput] = useState(urlQuery)

  useEffect(() => {
    document.title = 'Search'
  }, [])

  // Keep the box in sync if the URL changes (back/forward, direct link).
  useEffect(() => {
    setInput(urlQuery)
  }, [urlQuery])

  // Debounce input -> ?q so URL, history, and the fetch all update together.
  useEffect(() => {
    const trimmed = input.trim()
    if (trimmed === urlQuery) return
    const id = window.setTimeout(() => {
      setParams(trimmed ? { q: trimmed } : {}, { replace: true })
    }, 300)
    return () => window.clearTimeout(id)
  }, [input, urlQuery, setParams])

  const query = useSearch(urlQuery)
  const hasQuery = urlQuery.trim().length > 0

  return (
    <div className="search">
      <header className="page-header">
        <h1>Search</h1>
      </header>

      <form
        className="search-form"
        role="search"
        onSubmit={(e) => {
          e.preventDefault()
          setParams(input.trim() ? { q: input.trim() } : {}, { replace: true })
        }}
      >
        <label htmlFor="search-input" className="visually-hidden">
          Search posts
        </label>
        <input
          id="search-input"
          type="search"
          className="search-input"
          placeholder="Search posts…"
          value={input}
          autoComplete="off"
          autoFocus
          onChange={(e) => setInput(e.target.value)}
        />
      </form>

      {!hasQuery ? (
        <p className="muted">Type a word to search posts by title, body, or tag.</p>
      ) : (
        <PostResults query={query} emptyMessage={`No results for “${urlQuery}”.`} />
      )}
    </div>
  )
}
