// Home: Hero / Stack / Latest posts, composed from resume.json + settings +
// recent posts (no new content type). Selected-work moved to /works and the
// contact section moved to /contact. New resume fields are optional; sections
// whose source is absent are omitted entirely.

import { Fragment, useEffect } from 'react'
import { Link } from 'react-router-dom'

import { useRecentPosts, useResume, useSettings } from '../lib/queries'
import type {
  GenericSection,
  ResumeBasics,
  SkillGroup,
  SocialLink,
} from '../lib/types'
import { PendingState } from '../components/QueryStates'
import { PostListSkeleton } from '../components/Skeletons'
import PostCard from '../components/PostCard'

// Single source of truth for how many posts the home page previews.
const HOME_LATEST_POSTS = 5

export default function Home() {
  const settings = useSettings()
  const resume = useResume()
  const recent = useRecentPosts(HOME_LATEST_POSTS)

  useEffect(() => {
    document.title = settings.data?.title ?? 'Himanshu Raj Aman'
  }, [settings.data?.title])

  // The page is useful as soon as settings OR resume is available; block only
  // while everything we need is still pending.
  if (settings.isPending && resume.isPending) {
    return <PendingState failureReason={settings.failureReason ?? resume.failureReason} />
  }

  const sections = resume.data?.sections ?? []
  const basics = (sections.find((s) => s.kind === 'basics')?.data ?? {}) as ResumeBasics
  const skills = sections.find((s) => s.kind === 'skills')?.data as
    | GenericSection
    | undefined

  // Hero h1 fallback chain.
  const headline =
    basics.headline ?? basics.label ?? settings.data?.title ?? 'Himanshu Raj Aman'

  // Role line = "<label> — <location>" (either alone), rendered only if it
  // differs from the h1 (so it never duplicates a label used as the headline).
  const role = [basics.label, basics.location].filter(Boolean).join(' — ')
  const showRole = role.length > 0 && role !== headline

  const intro = basics.summary ?? settings.data?.description ?? ''

  // Secondary buttons: settings.social when present, else basics.links.
  const social: SocialLink[] =
    settings.data?.social?.length ? settings.data.social : basics.links ?? []

  const skillGroups = (skills?.groups ?? []) as SkillGroup[]

  const posts = recent.data?.items ?? []

  return (
    <div className="mx-auto max-w-page">
      <section className="pt-14 pb-10">
        <h1 className="text-5xl font-bold leading-[1.1] tracking-tight">
          {headline}
        </h1>
        {showRole && <p className="mt-3 text-lg text-muted">{role}</p>}
        {intro && <p className="mt-6 max-w-[60ch]">{intro}</p>}
        <div className="mt-7 flex flex-wrap gap-2.5">
          <Link
            to="/resume"
            className="border border-text bg-text px-4 py-2 text-sm text-white"
          >
            Resume
          </Link>
          {social.map((s) => (
            <a
              key={s.url}
              href={s.url}
              className="border border-line bg-surface px-4 py-2 text-sm text-text hover:border-line-strong"
              rel="me noopener noreferrer"
              target="_blank"
            >
              {s.label}
            </a>
          ))}
        </div>
      </section>

      {skills && skillGroups.length > 0 && (
        <section
          className="border-t border-line py-10"
          aria-labelledby="stack-heading"
        >
          <h2
            id="stack-heading"
            className="mb-6 text-[13px] font-semibold uppercase tracking-[0.08em] text-muted"
          >
            Stack
          </h2>
          <dl className="grid grid-cols-1 gap-x-4 gap-y-2.5 text-[15px] sm:grid-cols-[140px_1fr]">
            {skillGroups.map((group, i) => (
              <Fragment key={`${group.name ?? ''}-${i}`}>
                <dt className="text-muted">{group.name}</dt>
                <dd className="m-0">{(group.items ?? []).join(', ')}</dd>
              </Fragment>
            ))}
          </dl>
        </section>
      )}

      <section
        className="border-t border-line py-10"
        aria-labelledby="latest-heading"
      >
        <div className="mb-6 flex items-baseline justify-between gap-4">
          <h2
            id="latest-heading"
            className="text-[13px] font-semibold uppercase tracking-[0.08em] text-muted"
          >
            Latest posts
          </h2>
          <Link to="/blog" className="text-sm text-accent hover:underline">
            View all →
          </Link>
        </div>
        {recent.isPending ? (
          <PostListSkeleton count={HOME_LATEST_POSTS} />
        ) : recent.isError ? (
          <p className="text-muted">Couldn’t load posts right now.</p>
        ) : posts.length === 0 ? (
          <p className="text-muted">No posts yet.</p>
        ) : (
          <div className="flex flex-col gap-4">
            {posts.map((post) => (
              <PostCard key={post.slug} post={post} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
