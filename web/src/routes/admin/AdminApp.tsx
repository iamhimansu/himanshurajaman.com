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
import { Link, NavLink, Navigate, Route, Routes, useNavigate } from 'react-router-dom'

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
import {
  btn,
  btnPrimary,
  sidebar,
  sidebarLink,
  sidebarLinkActive,
  sidebarLinkIdle,
  subhead,
  mainWrap,
} from './ui'

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
      <AdminScreen role="status">
        <p className="text-muted">Loading admin…</p>
      </AdminScreen>
    )
  }

  if (state === 'auth-unavailable') {
    return (
      <AdminScreen role="alert">
        <p className="mb-1 font-semibold">Sign-in is temporarily unavailable</p>
        <p className="text-muted">
          The sign-in service can’t be reached right now. Please try again in a
          moment.
        </p>
        <p className="mt-4">
          <button type="button" className={btn} onClick={() => window.location.reload()}>
            Retry
          </button>
        </p>
      </AdminScreen>
    )
  }

  if (state === 'error') {
    return (
      <AdminScreen role="alert">
        <p className="mb-1 font-semibold">Something went wrong</p>
        <p className="text-muted">{errorMessage || 'Please try again in a moment.'}</p>
      </AdminScreen>
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
        <Route
          path="login"
          element={
            <AdminScreen>
              <Login />
            </AdminScreen>
          }
        />
        <Route
          path="posts"
          element={
            <Protected authed={state === 'authed'}>
              <AdminShell>
                <PostList />
              </AdminShell>
            </Protected>
          }
        />
        <Route
          path="posts/new"
          element={
            <Protected authed={state === 'authed'}>
              <AdminShell>
                <PostEditor mode="create" />
              </AdminShell>
            </Protected>
          }
        />
        <Route
          path="posts/:slug/edit"
          element={
            <Protected authed={state === 'authed'}>
              <AdminShell>
                <PostEditor mode="edit" />
              </AdminShell>
            </Protected>
          }
        />
        <Route
          path="resume"
          element={
            <Protected authed={state === 'authed'}>
              <AdminShell>
                <ResumeEditor />
              </AdminShell>
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

// A single centered bordered panel for the guard/login screens (not two-column
// — there is no authed session to navigate yet).
function AdminScreen({
  children,
  role,
}: {
  children: ReactNode
  role?: 'status' | 'alert'
}) {
  return (
    <div className="py-8">
      <div
        className="mx-auto max-w-md border border-line bg-surface p-6"
        role={role}
        aria-live={role === 'status' ? 'polite' : undefined}
      >
        {children}
      </div>
    </div>
  )
}

// Full-viewport admin shell: a persistent LEFT sidebar (site label, signed-in
// email, View site, Logout, Posts/Resume nav, New post) and a WIDE main panel
// holding the routed view. On lg it is a two-column grid with a sticky
// full-height sidebar; below lg the sidebar stacks ABOVE the content so 360px
// stays usable. Breaks out of the narrow public column (App.tsx routes it as a
// sibling of the public Layout group).
function AdminShell({ children }: { children: ReactNode }) {
  const { writesEnabled, me, signOut } = useAdminAuth()
  const navigate = useNavigate()

  return (
    <div className="min-h-screen w-full bg-bg text-text">
      <div className="mx-auto max-w-[110rem] lg:grid lg:grid-cols-[16rem_minmax(0,1fr)]">
        <aside className={sidebar} aria-label="Admin">
          <div className="lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
            <div>
              <p className={subhead}>Admin</p>
              <Link
                to="/"
                className="mt-1 inline-block text-sm text-muted hover:text-text"
              >
                View site ↗
              </Link>
            </div>

            <nav className="mt-5 flex flex-col gap-1" aria-label="Admin sections">
              <AdminNavLink to="/admin/posts">Posts</AdminNavLink>
              <AdminNavLink to="/admin/resume">Resume</AdminNavLink>
            </nav>

            <button
              type="button"
              className={`${btnPrimary} mt-5 w-full`}
              disabled={!writesEnabled}
              onClick={() => navigate('/admin/posts/new')}
            >
              New post
            </button>

            <div className="mt-5 border-t border-line pt-4 lg:mt-auto">
              {me && (
                <p className="break-words text-xs text-muted" title={me.email}>
                  {me.email}
                </p>
              )}
              <button
                type="button"
                className={`${btn} mt-2 w-full`}
                onClick={() => void signOut()}
              >
                Logout
              </button>
            </div>
          </div>
        </aside>
        <main className={mainWrap}>{children}</main>
      </div>
    </div>
  )
}

function AdminNavLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <NavLink
      to={to}
      end
      className={({ isActive }) =>
        `${sidebarLink} ${isActive ? sidebarLinkActive : sidebarLinkIdle}`
      }
    >
      {children}
    </NavLink>
  )
}
