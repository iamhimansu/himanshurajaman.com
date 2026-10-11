// Shared Tailwind class strings for the admin UI. Centralised so the bordered,
// token-driven look stays consistent across the shell, lists and editors
// without duplicating long utility strings. Imported only from the admin
// chunk, so these never touch the visitor bundle. Buttons/forms/badges live
// here as utilities; admin.css keeps only the Editor.js/third-party chrome.

/** Secondary / neutral button: bordered surface, hover darkens the border. */
export const btn =
  'inline-flex items-center justify-center border border-line bg-surface px-3 py-1.5 text-sm text-text hover:border-line-strong disabled:cursor-not-allowed disabled:opacity-50'

/** Primary action: dark fill. */
export const btnPrimary =
  'inline-flex items-center justify-center border border-text bg-text px-3 py-1.5 text-sm text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50'

/** A bordered panel surface (shell columns, cards). */
export const panel = 'border border-line bg-surface'

/** A labelled form field wrapper (stacked label + control). */
export const field = 'flex flex-col gap-1.5'

/** The small bold muted label above a field. */
export const fieldLabel = 'text-[13px] font-semibold text-muted'

/** Text input / textarea styling (rectangular, token colours). */
export const input =
  'w-full border border-line bg-surface px-3 py-2 text-[15px] text-text disabled:bg-raise disabled:text-muted'

/** Section sub-heading (uppercase label). */
export const subhead =
  'text-[13px] font-semibold uppercase tracking-[0.08em] text-muted'

/** Read-only / informational banner. */
export const banner =
  'border border-line bg-raise px-3.5 py-2.5 text-sm text-muted'

/** A success / neutral notice. */
export const notice =
  'border border-line bg-raise px-3.5 py-2.5 text-sm text-text'

/** An error box. */
export const errorBox =
  'border border-[#fca5a5] bg-[#fef2f2] px-3.5 py-2.5 text-sm text-[#991b1b]'

/** An inline link-styled button (retry / reload). */
export const linkButton = 'text-accent underline hover:no-underline'

/* ---- Full-viewport admin shell ------------------------------------------ */

/** The left sidebar: bordered full-height column on lg, stacks on narrow. */
export const sidebar =
  'border-b border-line bg-surface p-5 lg:sticky lg:top-0 lg:flex lg:h-screen lg:flex-col lg:border-b-0 lg:border-r'

/** The wide main content region beside the sidebar. */
export const mainWrap = 'min-w-0 p-5 sm:p-8'

/** Sidebar nav link — shared base + active/idle variants (border-l accent). */
export const sidebarLink = 'block border-l-2 px-3 py-1.5 text-sm'
export const sidebarLinkActive = 'border-accent font-semibold text-accent'
export const sidebarLinkIdle = 'border-transparent text-muted hover:text-text'

/* ---- Posts table / gridview --------------------------------------------- */

/** The posts table shell (sharp 1px line borders). */
export const table = 'w-full border-collapse border border-line text-sm'

/** A table header cell (uppercase mono-ish label, raised fill). */
export const th =
  'border border-line bg-raise px-3 py-2 text-left text-[12px] font-semibold uppercase tracking-[0.06em] text-muted'

/** A table body cell. */
export const td = 'border border-line px-3 py-3 align-top text-text'

/** Subtle gray row hover (reuses the FEAT-001 raise token). */
export const rowHover = 'hover:bg-raise'

/** Status badges — NOT colour-reliant: published is a dark fill, draft a
 *  bordered muted chip. Both sharp + uppercase mono. */
export const badgePublished =
  'inline-block border border-text bg-text px-2 py-0.5 font-mono text-[11px] uppercase tracking-[0.04em] text-white'
export const badgeDraft =
  'inline-block border border-line bg-surface px-2 py-0.5 font-mono text-[11px] uppercase tracking-[0.04em] text-muted'

/** Sharp bordered tag chip (mirrors the FEAT-001 public chip style). */
export const tagChip = 'inline-block border border-line px-2 py-0.5 font-mono text-xs text-muted'
