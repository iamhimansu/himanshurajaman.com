// Works: the resume's "projects" section surfaced as its own page (moved off
// Home). Each project is a bordered card; an optional "All repositories" link
// follows. Reuses /api/resume — no new content type.

import { useEffect } from 'react'

import { useResume } from '../lib/queries'
import type { GenericSection, ProjectItem } from '../lib/types'
import { ErrorState } from '../components/QueryStates'
import { WorksSkeleton } from '../components/Skeletons'

export default function Works() {
  const query = useResume()

  useEffect(() => {
    document.title = 'Works'
  }, [])

  const projects = query.data?.sections.find((s) => s.kind === 'projects')?.data as
    | GenericSection
    | undefined
  const items = (projects?.items ?? []) as ProjectItem[]
  const link = projects?.link

  return (
    <div className="mx-auto max-w-page">
      <header className="mb-6">
        <h1 className="text-3xl font-bold tracking-tight">Works</h1>
        <p className="mt-1 text-muted">Projects, experiments, and things I’ve shipped.</p>
      </header>

      {query.isPending ? (
        <WorksSkeleton />
      ) : query.isError ? (
        <ErrorState message={query.error.message} />
      ) : items.length === 0 ? (
        <p className="text-muted">No projects to show yet.</p>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {items.map((item, i) => (
              <ProjectCard key={`${item.name ?? ''}-${i}`} item={item} />
            ))}
          </div>
          {link && (
            <p className="mt-6 text-sm">
              <a
                href={link.url}
                className="text-accent hover:underline"
                rel="noopener noreferrer"
                target="_blank"
              >
                {link.label} →
              </a>
            </p>
          )}
        </>
      )}
    </div>
  )
}

function ProjectCard({ item }: { item: ProjectItem }) {
  return (
    <article className="flex flex-col border border-line p-4">
      <div className="flex items-baseline justify-between gap-4">
        <h2 className="text-base font-semibold text-text">
          {item.url ? (
            <a
              href={item.url}
              className="hover:text-accent"
              rel="noopener noreferrer"
              target="_blank"
            >
              {item.name}
            </a>
          ) : (
            item.name
          )}
        </h2>
        {item.tag && (
          <span className="shrink-0 font-mono text-xs text-muted">{item.tag}</span>
        )}
      </div>
      {item.description && (
        <p className="mt-2 text-sm text-muted">{item.description}</p>
      )}
    </article>
  )
}
