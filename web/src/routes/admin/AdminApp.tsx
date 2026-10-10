// Admin SPA shell: nested router + auth guard. This module (and everything it
// imports — adminApi, PostEditor) is the ONE admin entry; App.tsx lazy-loads it
// via React.lazy, so Vite keeps it in a separate chunk that the visitor entry
// bundle never imports. Editor.js itself is split one level further: EditorBody
// loads it via a dynamic import('./editor'), so it lands in its own chunk.
//
// On mount the guard calls GET /api/admin/me:
//   200 -> authed (store email/csrfToken/writesEnabled in context)
//   401 -> unauthenticated (routes redirect to /admin/login)
//   503 "admin disabled" -> disabled screen (Sign-in hidden)
//   503 "auth temporarily unavailable" -> friendly retry screen

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom'

import {
  AdminAuthUnavailableError,
  AdminDisabledError,
  AdminUnauthorizedError,
  configureAdminApi,
  fetchMe,
  logout as apiLogout,
} from '../../lib/adminApi'
import type { AdminMe } from '../../lib/types'
import Login from './Login'
import PostList from './PostList'
import PostEditor from './PostEditor'
import ResumeEditor from './ResumeEditor'

import '../../styles/admin.css'

type GuardState = 'loading' | 'authed' | 'login' | 'disabled' | 'auth-unavailable' | 'error'

interface AdminAuthValue {
  me: AdminMe | null
  disabled: boolean
  writesEnabled: boolean
  refresh: () => Promise<void>
  signOut: () => Promise<void>
}

const AdminAuthContext = createContext<AdminAuthValue | null>(null)

export function useAdminAuth(): AdminAuthValue {
  const ctx = useContext(AdminAuthContext)
  if (!ctx) throw new Error('useAdminAuth must be used inside AdminApp')
  return ctx
}

export default function AdminApp() {
  const [state, setState] = useState<GuardState>('loading')
  const [errorMessage, setErrorMessage] = useState<string>('')
  const [me, setMe] = useState<AdminMe | null>(null)
  const navigate = useNavigate()

  // Keep the latest token/navigate in refs so the adminApi wiring (configured
  // once) always reads current values without re-registering.
  const csrfTokenRef = useRef<string>('')
  const navigateRef = useRef(navigate)
  navigateRef.current = navigate

  useEffect(() => {
    document.title = 'Admin'
  }, [])

  // Load /api/admin/me and resolve the guard state. Returns the token on auth.
  const loadMe = useCallback(async (signal?: AbortSignal): Promise<AdminMe> => {
    const data = await fetchMe(signal)
    csrfTokenRef.current = data.csrfToken
    setMe(data)
    setState('authed')
    return data
  }, [])

  // Wire the adminApi wrapper once: current token, token refresh on 403 csrf,
  // and the 401 -> login redirect.
  useEffect(() => {
    configureAdminApi({
      getCsrfToken: () => csrfTokenRef.current,
      refreshCsrfToken: async () => {
        const data = await fetchMe()
        csrfTokenRef.current = data.csrfToken
        setMe(data)
        return data.csrfToken
      },
      onUnauthorized: () => {
        csrfTokenRef.current = ''
        setMe(null)
        setState('login')
        navigateRef.current('/admin/login', { replace: true })
      },
    })
  }, [])

  // Initial guard check.
  useEffect(() => {
    const controller = new AbortController()
    loadMe(controller.signal).catch((err) => {
      if (controller.signal.aborted) return
      if (err instanceof AdminUnauthorizedError) {
        setState('login')
      } else if (err instanceof AdminDisabledError) {
        setState('disabled')
      } else if (err instanceof AdminAuthUnavailableError) {
        setState('auth-unavailable')
      } else {
        setErrorMessage(err instanceof Error ? err.message : 'Failed to load admin')
        setState('error')
      }
    })
    return () => controller.abort()
  }, [loadMe])

  const refresh = useCallback(async () => {
    await loadMe()
  }, [loadMe])

  const signOut = useCallback(async () => {
    try {
      await apiLogout()
    } catch {
      // Even if logout fails server-side, drop the client session view.
    }
    csrfTokenRef.current = ''
    setMe(null)
    setState('login')
    navigateRef.current('/admin/login', { replace: true })
  }, [])

  if (state === 'loading') {
    return (
      <div className="state" role="status" aria-live="polite">
        <p className="muted">Loading admin…</p>
      </div>
    )
  }

  if (state === 'auth-unavailable') {
    return (
      <div className="state" role="alert">
        <p className="state-title">Sign-in is temporarily unavailable</p>
        <p className="muted">
          The sign-in service can’t be reached right now. Please try again in a
          moment.
        </p>
        <p>
          <button type="button" className="btn" onClick={() => window.location.reload()}>
            Retry
          </button>
        </p>
      </div>
    )
  }

  if (state === 'error') {
    return (
      <div className="state" role="alert">
        <p className="state-title">Something went wrong</p>
        <p className="muted">{errorMessage || 'Please try again in a moment.'}</p>
      </div>
    )
  }

  const disabled = state === 'disabled'
  const value: AdminAuthValue = {
    me,
    disabled,
    writesEnabled: me?.writesEnabled ?? false,
    refresh,
    signOut,
  }

  return (
    <AdminAuthContext.Provider value={value}>
      <Routes>
        <Route index element={<IndexRedirect authed={state === 'authed'} />} />
        <Route path="login" element={<Login />} />
        <Route
          path="posts"
          element={
            <Protected authed={state === 'authed'}>
              <PostList />
            </Protected>
          }
        />
        <Route
          path="posts/new"
          element={
            <Protected authed={state === 'authed'}>
              <PostEditor mode="create" />
            </Protected>
          }
        />
        <Route
          path="posts/:slug/edit"
          element={
            <Protected authed={state === 'authed'}>
              <PostEditor mode="edit" />
            </Protected>
          }
        />
        <Route
          path="resume"
          element={
            <Protected authed={state === 'authed'}>
              <ResumeEditor />
            </Protected>
          }
        />
        <Route path="*" element={<IndexRedirect authed={state === 'authed'} />} />
      </Routes>
    </AdminAuthContext.Provider>
  )
}

function IndexRedirect({ authed }: { authed: boolean }) {
  return <Navigate to={authed ? '/admin/posts' : '/admin/login'} replace />
}

function Protected({ authed, children }: { authed: boolean; children: ReactNode }) {
  if (!authed) return <Navigate to="/admin/login" replace />
  return <>{children}</>
}
