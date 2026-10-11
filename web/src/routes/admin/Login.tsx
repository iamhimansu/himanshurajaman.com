// Admin sign-in screen. Sign-in is a FULL-PAGE navigation (<a href>, not fetch)
// to GET /auth/login, which sets the sealed OAuth cookie and 302s to Google.
// When admin is not configured on this server the guard reports "disabled" and
// we hide the button and explain instead of rendering raw 503 JSON.

import { useEffect } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'

import { useAdminAuth } from './AdminApp'
import { btnPrimary, errorBox } from './ui'

export default function Login() {
  const { me, disabled } = useAdminAuth()
  const [params] = useSearchParams()

  useEffect(() => {
    document.title = 'Sign in · Admin'
  }, [])

  // Already signed in — go straight to the post list.
  if (me && !disabled) {
    return <Navigate to="/admin/posts" replace />
  }

  const errorCode = params.get('error')
  let errorMessage: string | null = null
  if (errorCode === 'forbidden') {
    errorMessage = 'That Google account isn’t authorized for this site.'
  } else if (errorCode === 'auth') {
    errorMessage = 'Sign-in failed, please try again.'
  }

  return (
    <section>
      <h2 className="mb-3 text-2xl font-semibold tracking-tight">Admin</h2>
      {disabled ? (
        <p className="text-muted">Admin is not configured on this server.</p>
      ) : (
        <>
          {errorMessage && (
            <p className={`mb-4 ${errorBox}`} role="alert">
              {errorMessage}
            </p>
          )}
          <p className="text-muted">
            Sign in with the authorized Google account to manage content.
          </p>
          <p className="mt-4">
            <a className={btnPrimary} href="/auth/login">
              Sign in with Google
            </a>
          </p>
        </>
      )}
    </section>
  )
}
