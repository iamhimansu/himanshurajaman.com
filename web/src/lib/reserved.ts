// Reserved slugs a page may not use, because they collide with existing routes
// or asset prefixes. This mirrors the server's content.ReservedSlugs list
// (internal/content/pagefile.go) VERBATIM, so the client-side guard in the
// public page view and the admin page editor rejects the same slugs the server
// rejects (defense in depth — the server is still the source of truth via
// ParsePage). Comparisons are lowercased, matching IsReservedSlug.
export const RESERVED: ReadonlySet<string> = new Set([
  'blog',
  'resume',
  'works',
  'contact',
  'admin',
  'api',
  'rss',
  'sitemap',
  'robots',
  'healthz',
  'search',
  'auth',
  'webhook',
  'images',
  'assets',
])

/** True when slug (case-insensitive, trimmed) is a reserved word. */
export function isReservedSlug(slug: string): boolean {
  return RESERVED.has(slug.trim().toLowerCase())
}
