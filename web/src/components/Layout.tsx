// App shell: a centered wide frame (max-w-wide, 64rem) holding the skip link, the
// fixed top nav, main, and footer. The nav is a hardcoded five-item set
// (independent of settings.nav); settings still supplies the brand title,
// author, social links and the runtime accent. The accent is applied to :root
// as a CSS custom property so tokens resolve to it everywhere.

import { useEffect } from 'react'
import { NavLink, Outlet, Link, useLocation } from 'react-router-dom'

import { useSettings } from '../lib/queries'
import type { SocialLink } from '../lib/types'

// Fixed primary navigation. Not derived from settings.nav by design.
const NAV = [
  { label: 'Home', path: '/' },
  { label: 'Works', path: '/works' },
  { label: 'Blog', path: '/blog' },
  { label: 'Resume', path: '/resume' },
  { label: 'Contact', path: '/contact' },
] as const

export default function Layout() {
  const { data: settings } = useSettings()
  const { pathname } = useLocation()
  const isHome = pathname === '/'

  const social: SocialLink[] = settings?.social ?? []
  const siteTitle = settings?.title ?? 'Himanshu Raj Aman'
  const author = settings?.author ?? siteTitle

  // Apply the runtime accent (sensible default lives in index.css).
  useEffect(() => {
    if (settings?.accent) {
      document.documentElement.style.setProperty('--accent', settings.accent)
    }
  }, [settings?.accent])

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-wide flex-col px-6">
      <a className="skip-link" href="#main">
        Skip to content
      </a>

      <header className="site-header flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-b border-line py-5">
        <Link to="/" className="text-base font-bold tracking-tight text-text">
          {siteTitle}
        </Link>
        <nav
          aria-label="Primary"
          className="flex flex-wrap items-center divide-x divide-line text-sm"
        >
          {NAV.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) =>
                `px-3 first:pl-0 ${
                  isActive
                    ? 'font-semibold text-accent'
                    : 'text-muted hover:text-text'
                }`
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      </header>

      <main
        id="main"
        className={`flex-1 ${isHome ? '' : 'pt-10'}`}
        tabIndex={-1}
      >
        <Outlet />
      </main>

      <footer className="mt-10 border-t border-line pt-10 pb-16 text-sm text-muted">
        © {new Date().getFullYear()} {author}
        {social.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-4">
            {social.map((s) => (
              <li key={s.url}>
                <a
                  href={s.url}
                  className="hover:text-text"
                  rel="me noopener noreferrer"
                  target="_blank"
                >
                  {s.label}
                </a>
              </li>
            ))}
          </ul>
        )}
      </footer>
    </div>
  )
}
