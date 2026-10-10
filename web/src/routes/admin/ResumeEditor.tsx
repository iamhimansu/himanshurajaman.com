// Resume editor. A structured form (basics + skills + projects) is the primary
// mode; a validated raw-JSON mode is the fallback for everything else and for
// power edits. Client-side checks give quick feedback, but the server's 400 is
// the authority. Save -> PUT /api/admin/resume with fileSha, with the same
// conflict/GitHub/writes-disabled surfacing as the post editor.

import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import {
  AdminApiError,
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWritesDisabledError,
  getResume,
  updateResume,
} from '../../lib/adminApi'
import { useAdminAuth } from './AdminApp'

interface ResumeLink {
  label?: string
  url?: string
}
interface ResumeBasicsDoc {
  name?: string
  headline?: string
  label?: string
  email?: string
  location?: string
  summary?: string
  links?: ResumeLink[]
  [k: string]: unknown
}
interface ResumeGroupDoc {
  name?: string
  items?: string[]
  [k: string]: unknown
}
interface ResumeProjectDoc {
  name?: string
  tag?: string
  url?: string
  description?: string
  [k: string]: unknown
}
interface ResumeSectionDoc {
  kind?: string
  title?: string
  items?: unknown[]
  groups?: ResumeGroupDoc[]
  link?: ResumeLink
  [k: string]: unknown
}
interface ResumeDoc {
  basics?: ResumeBasicsDoc
  sections?: ResumeSectionDoc[]
  [k: string]: unknown
}

type Mode = 'form' | 'json'

export default function ResumeEditor() {
  const { writesEnabled } = useAdminAuth()
  const navigate = useNavigate()
  const readOnly = !writesEnabled

  const [doc, setDoc] = useState<ResumeDoc>({})
  const [jsonText, setJsonText] = useState('')
  const [fileSha, setFileSha] = useState('')
  const [mode, setMode] = useState<Mode>('form')

  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [loadError, setLoadError] = useState('')

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saveNotice, setSaveNotice] = useState('')
  const [conflict, setConflict] = useState(false)

  useEffect(() => {
    document.title = 'Resume · Admin'
  }, [])

  const load = useCallback((signal?: AbortSignal) => {
    setLoadState('loading')
    getResume(signal)
      .then((res) => {
        if (signal?.aborted) return
        const parsed = (res.resume ?? {}) as ResumeDoc
        setDoc(parsed)
        setJsonText(JSON.stringify(parsed, null, 2))
        setFileSha(res.fileSha)
        setLoadState('ready')
      })
      .catch((err) => {
        if (signal?.aborted) return
        if (err instanceof AdminUnauthorizedError) return
        setLoadError(err instanceof Error ? err.message : 'Failed to load resume')
        setLoadState('error')
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  // Keep basics convenient to edit.
  const basics = doc.basics ?? {}
  const sections = doc.sections ?? []
  const skillsIndex = sections.findIndex((s) => s.kind === 'skills')
  const projectsIndex = sections.findIndex((s) => s.kind === 'projects')

  const updateBasics = useCallback((key: keyof ResumeBasicsDoc, value: string) => {
    setDoc((prev) => ({ ...prev, basics: { ...(prev.basics ?? {}), [key]: value } }))
  }, [])

  const updateSection = useCallback((index: number, next: ResumeSectionDoc) => {
    setDoc((prev) => {
      const list = [...(prev.sections ?? [])]
      list[index] = next
      return { ...prev, sections: list }
    })
  }, [])

  // Switch modes, syncing the representation across.
  const switchMode = useCallback(
    (next: Mode) => {
      setSaveError('')
      if (next === mode) return
      if (next === 'json') {
        setJsonText(JSON.stringify(doc, null, 2))
        setMode('json')
      } else {
        try {
          const parsed = JSON.parse(jsonText) as ResumeDoc
          setDoc(parsed)
          setMode('form')
        } catch (err) {
          setSaveError(`Invalid JSON: ${err instanceof Error ? err.message : 'parse error'}`)
        }
      }
    },
    [mode, doc, jsonText],
  )

  const onSave = useCallback(async () => {
    setSaving(true)
    setSaveError('')
    setSaveNotice('')
    setConflict(false)

    let payload: ResumeDoc
    if (mode === 'json') {
      try {
        payload = JSON.parse(jsonText) as ResumeDoc
      } catch (err) {
        setSaveError(`Invalid JSON: ${err instanceof Error ? err.message : 'parse error'}`)
        setSaving(false)
        return
      }
    } else {
      payload = doc
    }

    // Quick client-side feedback; the server's 400 remains authoritative.
    if (!payload.basics?.name || !payload.basics?.email) {
      setSaveError('Resume needs at least basics.name and basics.email.')
      setSaving(false)
      return
    }

    try {
      const res = await updateResume({ resume: payload, fileSha })
      setFileSha(res.fileSha)
      setDoc(payload)
      setJsonText(JSON.stringify(payload, null, 2))
      setSaveNotice('Saved.')
    } catch (err) {
      if (err instanceof AdminStaleError) {
        setConflict(true)
        setSaveError(
          'The resume changed on the server; reload to get the latest, then re-apply your edits.',
        )
      } else if (err instanceof AdminGitHubError) {
        setSaveError(`Couldn’t save to GitHub: ${err.message}`)
      } else if (err instanceof AdminWritesDisabledError) {
        setSaveError('This server is read-only (local content source).')
      } else if (!(err instanceof AdminUnauthorizedError)) {
        setSaveError(err instanceof AdminApiError ? err.message : 'Save failed')
      }
    } finally {
      setSaving(false)
    }
  }, [mode, jsonText, doc, fileSha])

  if (loadState === 'loading') {
    return (
      <div className="state" role="status" aria-live="polite">
        <p className="muted">Loading resume…</p>
      </div>
    )
  }
  if (loadState === 'error') {
    return (
      <div className="state" role="alert">
        <p className="state-title">Couldn’t load the resume</p>
        <p className="muted">{loadError}</p>
        <p>
          <button type="button" className="btn" onClick={() => load()}>
            Retry
          </button>
        </p>
      </div>
    )
  }

  return (
    <section className="admin-resume">
      <div className="admin-bar">
        <h2>Resume</h2>
        <div className="admin-bar-actions">
          <button type="button" className="btn" onClick={() => navigate('/admin/posts')}>
            Back
          </button>
          <button
            type="button"
            className={mode === 'form' ? 'btn primary' : 'btn'}
            onClick={() => switchMode('form')}
          >
            Form
          </button>
          <button
            type="button"
            className={mode === 'json' ? 'btn primary' : 'btn'}
            onClick={() => switchMode('json')}
          >
            JSON
          </button>
          <button
            type="button"
            className="btn primary"
            disabled={readOnly || saving}
            onClick={() => void onSave()}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {readOnly && (
        <p className="admin-banner" role="status">
          This server is read-only (local content source).
        </p>
      )}
      {saveNotice && (
        <p className="admin-notice" role="status">
          {saveNotice}
        </p>
      )}
      {saveError && (
        <p className="admin-error" role="alert">
          {saveError}
          {conflict && (
            <>
              {' '}
              <button type="button" className="link-button" onClick={() => load()}>
                Reload
              </button>
            </>
          )}
        </p>
      )}

      {mode === 'json' ? (
        <textarea
          className="admin-json"
          value={jsonText}
          disabled={readOnly}
          spellCheck={false}
          rows={28}
          aria-label="Resume JSON"
          onChange={(e) => setJsonText(e.target.value)}
        />
      ) : (
        <div className="admin-form">
          <h3 className="admin-subhead">Basics</h3>
          <label className="admin-field">
            <span>Name</span>
            <input
              type="text"
              value={basics.name ?? ''}
              disabled={readOnly}
              onChange={(e) => updateBasics('name', e.target.value)}
            />
          </label>
          <label className="admin-field">
            <span>Headline</span>
            <input
              type="text"
              value={basics.headline ?? ''}
              disabled={readOnly}
              onChange={(e) => updateBasics('headline', e.target.value)}
            />
          </label>
          <label className="admin-field">
            <span>Label</span>
            <input
              type="text"
              value={basics.label ?? ''}
              disabled={readOnly}
              onChange={(e) => updateBasics('label', e.target.value)}
            />
          </label>
          <label className="admin-field">
            <span>Email</span>
            <input
              type="email"
              value={basics.email ?? ''}
              disabled={readOnly}
              onChange={(e) => updateBasics('email', e.target.value)}
            />
          </label>
          <label className="admin-field">
            <span>Location</span>
            <input
              type="text"
              value={basics.location ?? ''}
              disabled={readOnly}
              onChange={(e) => updateBasics('location', e.target.value)}
            />
          </label>
          <label className="admin-field">
            <span>Summary</span>
            <textarea
              value={basics.summary ?? ''}
              rows={3}
              disabled={readOnly}
              onChange={(e) => updateBasics('summary', e.target.value)}
            />
          </label>

          {skillsIndex >= 0 && (
            <SkillsEditor
              section={sections[skillsIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(skillsIndex, next)}
            />
          )}
          {projectsIndex >= 0 && (
            <ProjectsEditor
              section={sections[projectsIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(projectsIndex, next)}
            />
          )}

          <p className="muted admin-hint">
            Experience, education, and any other sections are editable in JSON mode.
          </p>
        </div>
      )}
    </section>
  )
}

function SkillsEditor({
  section,
  readOnly,
  onChange,
}: {
  section: ResumeSectionDoc
  readOnly: boolean
  onChange: (next: ResumeSectionDoc) => void
}) {
  const groups = section.groups ?? []
  const setGroup = (i: number, patch: Partial<ResumeGroupDoc>) => {
    const next = [...groups]
    next[i] = { ...next[i], ...patch }
    onChange({ ...section, groups: next })
  }
  return (
    <>
      <h3 className="admin-subhead">{section.title ?? 'Skills'}</h3>
      {groups.map((group, i) => (
        <div className="admin-field" key={i}>
          <span>{group.name ?? `Group ${i + 1}`}</span>
          <input
            type="text"
            value={(group.items ?? []).join(', ')}
            disabled={readOnly}
            placeholder="Comma-separated"
            onChange={(e) =>
              setGroup(i, {
                items: e.target.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
          />
        </div>
      ))}
    </>
  )
}

function ProjectsEditor({
  section,
  readOnly,
  onChange,
}: {
  section: ResumeSectionDoc
  readOnly: boolean
  onChange: (next: ResumeSectionDoc) => void
}) {
  const items = (section.items ?? []) as ResumeProjectDoc[]
  const setItem = (i: number, patch: Partial<ResumeProjectDoc>) => {
    const next = [...items]
    next[i] = { ...next[i], ...patch }
    onChange({ ...section, items: next })
  }
  return (
    <>
      <h3 className="admin-subhead">{section.title ?? 'Projects'}</h3>
      {items.map((item, i) => (
        <div className="admin-project-row" key={i}>
          <label className="admin-field">
            <span>Name</span>
            <input
              type="text"
              value={item.name ?? ''}
              disabled={readOnly}
              onChange={(e) => setItem(i, { name: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span>Tag</span>
            <input
              type="text"
              value={item.tag ?? ''}
              disabled={readOnly}
              onChange={(e) => setItem(i, { tag: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span>URL</span>
            <input
              type="text"
              value={item.url ?? ''}
              disabled={readOnly}
              onChange={(e) => setItem(i, { url: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span>Description</span>
            <textarea
              value={item.description ?? ''}
              rows={2}
              disabled={readOnly}
              onChange={(e) => setItem(i, { description: e.target.value })}
            />
          </label>
        </div>
      ))}
    </>
  )
}
