// Catch-all 404 for unknown client routes.

import { useEffect } from 'react'
import { Link } from 'react-router-dom'

export default function NotFound() {
  useEffect(() => {
    document.title = 'Not found'
  }, [])

  return (
    <div className="mx-auto max-w-page py-6">
      <h1 className="text-3xl font-bold tracking-tight">
        Page not found
      </h1>
      <p className="mt-1.5 text-muted">The page you’re looking for doesn’t exist.</p>
      <p className="mt-3">
        <Link to="/" className="text-accent hover:underline">
          ← Back home
        </Link>
      </p>
    </div>
  )
}
