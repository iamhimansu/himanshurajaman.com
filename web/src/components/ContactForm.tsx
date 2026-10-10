// In-page contact form for the home Contact section. Posts to POST /api/contact
// (public, unauthenticated). Client-side validation mirrors the server, but the
// server is authoritative. A hidden honeypot (`website`) catches naive bots. The
// status line is an aria-live region so screen readers hear state changes.

import { useId, useRef, useState, type FormEvent } from 'react'

import { ApiError, postContact } from '../lib/api'

type Status = 'idle' | 'sending' | 'success' | 'error'

// Length caps mirror the server (internal/server/contact.go).
const MAX_NAME = 100
const MAX_EMAIL = 200
const MAX_MESSAGE = 5000

// Pragmatic email shape check; the server does the authoritative parse.
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export default function ContactForm() {
  const [status, setStatus] = useState<Status>('idle')
  const [message, setMessage] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [body, setBody] = useState('')
  const honeypotRef = useRef<HTMLInputElement>(null)

  const ids = useId()
  const nameId = `${ids}-name`
  const emailId = `${ids}-email`
  const bodyId = `${ids}-message`
  const hpId = `${ids}-website`

  const sending = status === 'sending'

  function validate(): string | null {
    const n = name.trim()
    const e = email.trim()
    const b = body.trim()
    if (!n) return 'Please enter your name.'
    if (n.length > MAX_NAME) return 'Name is too long (max 100 characters).'
    if (!e || !EMAIL_RE.test(e)) return 'Please enter a valid email address.'
    if (e.length > MAX_EMAIL) return 'Email is too long (max 200 characters).'
    if (!b) return 'Please enter a message.'
    if (b.length > MAX_MESSAGE) return 'Message is too long (max 5000 characters).'
    return null
  }

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (sending) return

    const invalid = validate()
    if (invalid) {
      setStatus('error')
      setMessage(invalid)
      return
    }

    setStatus('sending')
    setMessage('')
    try {
      await postContact({
        name: name.trim(),
        email: email.trim(),
        message: body.trim(),
        website: honeypotRef.current?.value ?? '',
      })
      setStatus('success')
      setMessage('Thanks — I’ll get back to you.')
      setName('')
      setEmail('')
      setBody('')
    } catch (err) {
      setStatus('error')
      if (err instanceof ApiError && err.status === 429) {
        setMessage('Too many messages, please try later.')
      } else if (err instanceof ApiError && err.status === 400) {
        setMessage(err.message || 'Please check your details and try again.')
      } else {
        setMessage('Something went wrong. Please try again, or email me directly.')
      }
    }
  }

  return (
    <div className="contact">
      {status !== 'success' && (
        <form className="contact-form" onSubmit={onSubmit} noValidate>
          <div className="contact-field">
            <label htmlFor={nameId}>Name</label>
            <input
              id={nameId}
              name="name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={MAX_NAME}
              autoComplete="name"
              required
              disabled={sending}
            />
          </div>

          <div className="contact-field">
            <label htmlFor={emailId}>Email</label>
            <input
              id={emailId}
              name="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              maxLength={MAX_EMAIL}
              autoComplete="email"
              required
              disabled={sending}
            />
          </div>

          <div className="contact-field">
            <label htmlFor={bodyId}>Message</label>
            <textarea
              id={bodyId}
              name="message"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              maxLength={MAX_MESSAGE}
              rows={5}
              required
              disabled={sending}
            />
          </div>

          {/* Honeypot: hidden off-screen (not display:none, which some bots skip)
              and removed from the accessibility tree. Humans never see or focus
              it; a filled value marks the submission as spam server-side. */}
          <div className="contact-hp" aria-hidden="true">
            <label htmlFor={hpId}>Website</label>
            <input
              id={hpId}
              name="website"
              type="text"
              ref={honeypotRef}
              tabIndex={-1}
              autoComplete="off"
            />
          </div>

          <div className="contact-actions">
            <button type="submit" className="btn primary" disabled={sending}>
              {sending ? 'Sending…' : 'Send message'}
            </button>
          </div>
        </form>
      )}

      <p
        className={status === 'error' ? 'contact-status contact-status-error' : 'contact-status'}
        role="status"
        aria-live="polite"
      >
        {status === 'success' || status === 'error' ? message : ''}
      </p>
    </div>
  )
}
