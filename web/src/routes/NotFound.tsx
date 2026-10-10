// Catch-all 404 for unknown client routes.

import { useEffect } from 'react'
import { Link } from 'react-router-dom'

export default function NotFound() {
  useEffect(() => {
    document.title = 'Not found'
  }, [])

  return (
    <div className="state">
      <h1>Page not found</h1>
      <p className="muted">The page you’re looking for doesn’t exist.</p>
      <p>
        <Link to="/">← Back home</Link>
      </p>
    </div>
  )
}
