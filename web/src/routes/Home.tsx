// Home: Hero / Selected work / Stack / Contact, composed from resume.json +
// settings (no new content type). Reproduces docs/design/prototype/index.html.
// All new resume fields are optional; sections whose source is absent are
// omitted entirely.

import { Fragment, useEffect } from 'react'
import { Link } from 'react-router-dom'

import { useResume, useSettings } from '../lib/queries'
import type {
  GenericSection,
  ProjectItem,
  ResumeBasics,
  SkillGroup,
  SocialLink,
} from '../lib/types'
import { PendingState } from '../components/QueryStates'
import ContactForm from '../components/ContactForm'

export default function Home() {
  const settings = useSettings()
  const resume = useResume()

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
  const projects = sections.find((s) => s.kind === 'projects')?.data as
    | GenericSection
    | undefined
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

  const projectItems = (projects?.items ?? []) as ProjectItem[]
  const projectsLink = projects?.link
  const skillGroups = (skills?.groups ?? []) as SkillGroup[]

  const contactLine = settings.data?.description ?? ''
  const contactLinks = social
  const ownerEmail = basics.email
  // Server-authoritative: show the form when contact is configured, else a
  // mailto fallback. Defaults to false so the form stays hidden until the
  // settings query resolves (and whenever SMTP is unset on the server).
  const contactEnabled = settings.data?.contactEnabled ?? false
  const showContact =
    contactEnabled || Boolean(contactLine) || contactLinks.length > 0 || Boolean(ownerEmail)

  return (
    <div className="home">
      <div className="hero">
        <h1>{headline}</h1>
        {showRole && <p className="role">{role}</p>}
        {intro && <p>{intro}</p>}
        <div className="links">
          <Link to="/resume" className="btn primary">
            Resume
          </Link>
          {social.map((s) => (
            <a
              key={s.url}
              href={s.url}
              className="btn"
              rel="me noopener noreferrer"
              target="_blank"
            >
              {s.label}
            </a>
          ))}
        </div>
      </div>

      {projects && projectItems.length > 0 && (
        <section id="work" aria-labelledby="work-heading">
          <h2 id="work-heading">Selected work</h2>
          {projectItems.map((item, i) =>
            item.url ? (
              <a
                key={`${item.name ?? ''}-${i}`}
                className="project"
                href={item.url}
                rel="noopener noreferrer"
                target="_blank"
              >
                <div className="row">
                  <h3>{item.name}</h3>
                  {item.tag && <span className="tag">{item.tag}</span>}
                </div>
                {item.description && <p>{item.description}</p>}
              </a>
            ) : (
              <div className="project" key={`${item.name ?? ''}-${i}`}>
                <div className="row">
                  <h3>{item.name}</h3>
                  {item.tag && <span className="tag">{item.tag}</span>}
                </div>
                {item.description && <p>{item.description}</p>}
              </div>
            ),
          )}
          {projectsLink && (
            <p className="more">
              <a href={projectsLink.url} rel="noopener noreferrer" target="_blank">
                {projectsLink.label} →
              </a>
            </p>
          )}
        </section>
      )}

      {skills && skillGroups.length > 0 && (
        <section id="stack" aria-labelledby="stack-heading">
          <h2 id="stack-heading">Stack</h2>
          <dl className="stack">
            {skillGroups.map((group, i) => (
              <Fragment key={`${group.name ?? ''}-${i}`}>
                <dt>{group.name}</dt>
                <dd>{(group.items ?? []).join(', ')}</dd>
              </Fragment>
            ))}
          </dl>
        </section>
      )}

      {showContact && (
        <section id="contact" aria-labelledby="contact-heading">
          <h2 id="contact-heading">Contact</h2>
          {contactLine && <p>{contactLine}</p>}
          {contactLinks.length > 0 && (
            <p>
              {contactLinks.map((s, i) => (
                <span key={s.url}>
                  {i > 0 && <span aria-hidden="true"> · </span>}
                  <a href={s.url} rel="me noopener noreferrer" target="_blank">
                    {s.label}
                  </a>
                </span>
              ))}
            </p>
          )}
          {contactEnabled ? (
            <ContactForm />
          ) : (
            ownerEmail && (
              <p className="contact-fallback">
                Prefer email? Reach me at{' '}
                <a href={`mailto:${ownerEmail}`}>{ownerEmail}</a>.
              </p>
            )
          )}
        </section>
      )}
    </div>
  )
}
