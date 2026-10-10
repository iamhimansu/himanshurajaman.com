// Resume: an HTML view of /api/resume, rendered section by section in the
// server's position order. Known kinds (basics, experience, education, skills,
// projects) get tailored layouts; unknown kinds render generically so new
// section types never break the page. styles/print.css makes browser Print
// produce a clean, link-expanded document.

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
import { ErrorState, PendingState } from '../components/QueryStates'

export default function Resume() {
  const query = useResume()
  const settings = useSettings()

  useEffect(() => {
    const name = settings.data?.author ?? settings.data?.title ?? 'Resume'
    document.title = `Resume · ${name}`
  }, [settings.data?.author, settings.data?.title])

  if (query.isPending) {
    return <PendingState failureReason={query.failureReason} />
  }
  if (query.isError) {
    return <ErrorState message={query.error.message} />
  }

  return (
    <div className="resume">
      <div className="resume-toolbar no-print">
        <button type="button" className="btn" onClick={() => window.print()}>
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
    <header className="resume-basics">
      {data.name && <h1>{data.name}</h1>}
      {data.label && <p className="resume-label">{data.label}</p>}
      <p className="muted resume-contact">
        {data.location && <span>{data.location}</span>}
        {data.location && data.email && <span aria-hidden="true"> · </span>}
        {data.email && <a href={`mailto:${data.email}`}>{data.email}</a>}
      </p>
      {data.summary && <p className="resume-summary">{data.summary}</p>}
      {data.links && data.links.length > 0 && (
        <ul className="inline-links" aria-label="Links">
          {data.links.map((link) => (
            <li key={link.url}>
              <a href={link.url} rel="noopener noreferrer" target="_blank">
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
  return <h2>{title ?? fallback}</h2>
}

function Experience({ data }: { data: GenericSection }) {
  const items = (data.items ?? []) as ExperienceItem[]
  return (
    <section>
      <SectionTitle title={data.title} fallback="Experience" />
      {items.map((item, i) => (
        <div className="resume-entry" key={`${item.org ?? ''}-${i}`}>
          <div className="resume-entry-head">
            <h3 className="resume-entry-title">
              {item.role}
              {item.role && item.org && <span className="muted"> · {item.org}</span>}
              {!item.role && item.org}
            </h3>
            <span className="muted resume-dates">{dateRange(item.start, item.end)}</span>
          </div>
          {item.location && <p className="muted resume-entry-sub">{item.location}</p>}
          {item.highlights && item.highlights.length > 0 && (
            <ul className="resume-highlights">
              {item.highlights.map((h, j) => (
                <li key={j}>{h}</li>
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
        <div className="resume-entry" key={`${item.org ?? ''}-${i}`}>
          <div className="resume-entry-head">
            <h3 className="resume-entry-title">
              {item.credential}
              {item.credential && item.org && <span className="muted"> · {item.org}</span>}
              {!item.credential && item.org}
            </h3>
            <span className="muted resume-dates">{dateRange(item.start, item.end)}</span>
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
      <dl className="stack">
        {groups.map((group, i) => (
          <Fragment key={`${group.name ?? ''}-${i}`}>
            <dt>{group.name}</dt>
            <dd>{(group.items ?? []).join(', ')}</dd>
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
            className="project"
            key={`${item.name ?? ''}-${i}`}
            href={item.url}
            rel="noopener noreferrer"
            target="_blank"
          >
            <div className="row">
              <h3>{item.name}</h3>
            </div>
            {item.description && <p>{item.description}</p>}
          </a>
        ) : (
          <div className="project" key={`${item.name ?? ''}-${i}`}>
            <div className="row">
              <h3>{item.name}</h3>
            </div>
            {item.description && <p>{item.description}</p>}
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
        <ul className="resume-highlights">
          {items.map((item, i) => (
            <li key={i}>{typeof item === 'string' ? item : JSON.stringify(item)}</li>
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
