// App shell: one centered .wrap holding the skip link, header/nav, main, and
// footer (matches docs/design/prototype/index.html). Nav and footer come from
// /api/settings when available, with a static fallback so the chrome renders
// even while the API is warming. The accent color is applied to :root as a CSS
// custom property so plain CSS can reference it everywhere.

import { useEffect } from 'react'
import { NavLink, Outlet, Link, useLocation } from 'react-router-dom'

import { useSettings } from '../lib/queries'
import type { NavLink as NavLinkT, SocialLink } from '../lib/types'

const FALLBACK_NAV: NavLinkT[] = [
  { label: 'Home', path: '/' },
  { label: 'Resume', path: '/resume' },
  { label: 'Blog', path: '/blog' },
]

export default function Layout() {
  const { data: settings } = useSettings()
  const { pathname } = useLocation()
  const isHome = pathname === '/'

  const nav = settings?.nav?.length ? settings.nav : FALLBACK_NAV
  const social: SocialLink[] = settings?.social ?? []
  const siteTitle = settings?.title ?? 'Himanshu Raj Aman'
  const author = settings?.author ?? siteTitle

  // Apply the runtime accent (sensible default lives in global.css).
  useEffect(() => {
    if (settings?.accent) {
      document.documentElement.style.setProperty('--accent', settings.accent)
    }
  }, [settings?.accent])

  return (
    <div className="wrap">
      <a className="skip-link" href="#main">
        Skip to content
      </a>

      <header className="top">
        <Link to="/" className="brand">
          {siteTitle}
        </Link>
        <nav aria-label="Primary">
          {nav.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) => (isActive ? 'active' : undefined)}
            >
              {item.label}
            </NavLink>
          ))}
          <NavLink
            to="/search"
            className={({ isActive }) => (isActive ? 'active' : undefined)}
          >
            Search
          </NavLink>
        </nav>
      </header>

      <main id="main" className={isHome ? undefined : 'page'} tabIndex={-1}>
        <Outlet />
      </main>

      <footer>
        © {new Date().getFullYear()} {author}
        {social.length > 0 && (
          <ul className="social-list">
            {social.map((s) => (
              <li key={s.url}>
                <a href={s.url} rel="me noopener noreferrer" target="_blank">
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
