// Resume: an HTML view of /api/resume, rendered section by section in the
// server's position order. Known kinds (basics, experience, education, skills,
// projects) get tailored layouts; unknown kinds render generically so new
// section types never break the page. styles/print.css makes browser Print
// produce a clean, link-expanded document — the `.resume` and `.no-print`
// class hooks below are what print.css targets.

import { Fragment, useEffect } from 'react'

import { useResume, useSettings } from '../lib/queries'
import type {
  EducationItem,
  ExperienceItem,
  GenericSection,
  ProjectItem,
  ResumeBasics,
  ResumeSection,
  SkillGroup,
} from '../lib/types'
import { ErrorState, Warming } from '../components/QueryStates'
import { WarmingError } from '../lib/api'
import { ResumeSkeleton } from '../components/Skeletons'

const SECTION_TITLE =
  'mb-6 text-[13px] font-semibold uppercase tracking-[0.08em] text-muted'
const ENTRY = 'border-b border-line py-[18px] last:border-b-0'
const ENTRY_HEAD = 'flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5'
const ENTRY_TITLE = 'm-0 text-[17px] font-semibold'
const DATES = 'whitespace-nowrap text-[15px] text-muted'

export default function Resume() {
  const query = useResume()
  const settings = useSettings()

  useEffect(() => {
    const name = settings.data?.author ?? settings.data?.title ?? 'Resume'
    document.title = `Resume · ${name}`
  }, [settings.data?.author, settings.data?.title])

  if (query.isPending) {
    if (query.failureReason instanceof WarmingError) return <Warming />
    return <ResumeSkeleton />
  }
  if (query.isError) {
    return <ErrorState message={query.error.message} />
  }

  return (
    <div className="resume mx-auto max-w-page [&_section]:border-t [&_section]:border-line [&_section]:py-10">
      <div className="no-print mb-2 flex justify-end print:hidden">
        <button
          type="button"
          className="border border-line bg-surface px-4 py-2 text-sm text-text hover:border-line-strong"
          onClick={() => window.print()}
        >
          Print
        </button>
      </div>
      {query.data.sections.map((section, i) => (
        <Section key={`${section.kind}-${i}`} section={section} />
      ))}
    </div>
  )
}

function Section({ section }: { section: ResumeSection }) {
  switch (section.kind) {
    case 'basics':
      return <Basics data={section.data as ResumeBasics} />
    case 'experience':
      return <Experience data={section.data as GenericSection} />
    case 'education':
      return <Education data={section.data as GenericSection} />
    case 'skills':
      return <Skills data={section.data as GenericSection} />
    case 'projects':
      return <Projects data={section.data as GenericSection} />
    default:
      return <Generic data={section.data as GenericSection} />
  }
}

function Basics({ data }: { data: ResumeBasics }) {
  return (
    <header className="pt-2">
      {data.name && (
        <h1 className="text-3xl font-bold leading-tight tracking-tight">
          {data.name}
        </h1>
      )}
      {data.label && <p className="mt-1 text-lg text-muted">{data.label}</p>}
      <p className="mb-2.5 mt-1 text-[15px] text-muted">
        {data.location && <span>{data.location}</span>}
        {data.location && data.email && <span aria-hidden="true"> · </span>}
        {data.email && (
          <a href={`mailto:${data.email}`} className="text-accent hover:underline">
            {data.email}
          </a>
        )}
      </p>
      {data.summary && <p className="mt-0">{data.summary}</p>}
      {data.links && data.links.length > 0 && (
        <ul className="mb-4 mt-2 flex flex-wrap gap-4" aria-label="Links">
          {data.links.map((link) => (
            <li key={link.url}>
              <a
                href={link.url}
                className="text-accent hover:underline"
                rel="noopener noreferrer"
                target="_blank"
              >
                {link.label}
              </a>
            </li>
          ))}
        </ul>
      )}
    </header>
  )
}

function SectionTitle({ title, fallback }: { title?: string; fallback: string }) {
  return <h2 className={SECTION_TITLE}>{title ?? fallback}</h2>
}

function Experience({ data }: { data: GenericSection }) {
  const items = (data.items ?? []) as ExperienceItem[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Experience" />
      {items.map((item, i) => (
        <div className={ENTRY} key={`${item.org ?? ''}-${i}`}>
          <div className={ENTRY_HEAD}>
            <h3 className={ENTRY_TITLE}>
              {item.role}
              {item.role && item.org && <span className="text-muted"> · {item.org}</span>}
              {!item.role && item.org}
            </h3>
            <span className={DATES}>{dateRange(item.start, item.end)}</span>
          </div>
          {item.location && (
            <p className="mt-1.5 text-[15px] text-muted">{item.location}</p>
          )}
          {item.highlights && item.highlights.length > 0 && (
            <ul className="mt-1.5 list-disc pl-5">
              {item.highlights.map((h, j) => (
                <li key={j} className="mb-1">
                  {h}
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
    </section>
  )
}

function Education({ data }: { data: GenericSection }) {
  const items = (data.items ?? []) as EducationItem[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Education" />
      {items.map((item, i) => (
        <div className={ENTRY} key={`${item.org ?? ''}-${i}`}>
          <div className={ENTRY_HEAD}>
            <h3 className={ENTRY_TITLE}>
              {item.credential}
              {item.credential && item.org && (
                <span className="text-muted"> · {item.org}</span>
              )}
              {!item.credential && item.org}
            </h3>
            <span className={DATES}>{dateRange(item.start, item.end)}</span>
          </div>
        </div>
      ))}
    </section>
  )
}

function Skills({ data }: { data: GenericSection }) {
  const groups = (data.groups ?? []) as SkillGroup[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Skills" />
      <dl className="grid grid-cols-1 gap-x-4 gap-y-2.5 text-[15px] sm:grid-cols-[140px_1fr]">
        {groups.map((group, i) => (
          <Fragment key={`${group.name ?? ''}-${i}`}>
            <dt className="text-muted">{group.name}</dt>
            <dd className="m-0">{(group.items ?? []).join(', ')}</dd>
          </Fragment>
        ))}
      </dl>
    </section>
  )
}

function Projects({ data }: { data: GenericSection }) {
  const items = (data.items ?? []) as ProjectItem[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Projects" />
      {items.map((item, i) =>
        item.url ? (
          <a
            className={`block ${ENTRY} [&:hover_h3]:text-accent`}
            key={`${item.name ?? ''}-${i}`}
            href={item.url}
            rel="noopener noreferrer"
            target="_blank"
          >
            <h3 className={ENTRY_TITLE}>{item.name}</h3>
            {item.description && (
              <p className="mt-1.5 text-[15px] text-muted">{item.description}</p>
            )}
          </a>
        ) : (
          <div className={ENTRY} key={`${item.name ?? ''}-${i}`}>
            <h3 className={ENTRY_TITLE}>{item.name}</h3>
            {item.description && (
              <p className="mt-1.5 text-[15px] text-muted">{item.description}</p>
            )}
          </div>
        ),
      )}
    </section>
  )
}

// Generic fallback for unknown kinds: show the title and any string items.
function Generic({ data }: { data: GenericSection }) {
  const items = (data.items ?? []) as unknown[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Section" />
      {items.length > 0 && (
        <ul className="mt-1.5 list-disc pl-5">
          {items.map((item, i) => (
            <li key={i} className="mb-1">
              {typeof item === 'string' ? item : JSON.stringify(item)}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function dateRange(start?: string, end?: string | null): string {
  if (!start && !end) return ''
  const to = end ? end : 'Present'
  return start ? `${start} – ${to}` : to
}
