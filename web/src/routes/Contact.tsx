// Contact: the contact section moved off Home onto its own page. Shows the
// in-page form when the server reports contact is configured, otherwise a
// mailto fallback built from the resume's basics.email. Social links and the
// site description give a lightweight alternative to reach out.

import { useEffect } from 'react'

import { useResume, useSettings } from '../lib/queries'
import type { ResumeBasics, SocialLink } from '../lib/types'
import ContactForm from '../components/ContactForm'

export default function Contact() {
  const settings = useSettings()
  const resume = useResume()

  useEffect(() => {
    document.title = 'Contact'
  }, [])

  const sections = resume.data?.sections ?? []
  const basics = (sections.find((s) => s.kind === 'basics')?.data ?? {}) as ResumeBasics
  const ownerEmail = basics.email

  const description = settings.data?.description ?? ''
  const social: SocialLink[] =
    settings.data?.social?.length ? settings.data.social : basics.links ?? []

  // Server-authoritative: show the form when contact is configured, else a
  // mailto fallback. Defaults to false until settings resolves.
  const contactEnabled = settings.data?.contactEnabled ?? false

  return (
    <div className="mx-auto max-w-page">
      <header className="mb-6">
        <h1 className="text-3xl font-bold tracking-tight">Contact</h1>
        {description && <p className="mt-1 text-muted">{description}</p>}
      </header>

      {social.length > 0 && (
        <p className="mb-6 text-sm">
          {social.map((s, i) => (
            <span key={s.url}>
              {i > 0 && <span aria-hidden="true"> · </span>}
              <a
                href={s.url}
                className="text-accent hover:underline"
                rel="me noopener noreferrer"
                target="_blank"
              >
                {s.label}
              </a>
            </span>
          ))}
        </p>
      )}

      {contactEnabled ? (
        <ContactForm />
      ) : ownerEmail ? (
        <p className="text-base">
          Prefer email? Reach me at{' '}
          <a href={`mailto:${ownerEmail}`} className="text-accent hover:underline">
            {ownerEmail}
          </a>
          .
        </p>
      ) : (
        <p className="text-muted">Contact is currently unavailable.</p>
      )}
    </div>
  )
}
