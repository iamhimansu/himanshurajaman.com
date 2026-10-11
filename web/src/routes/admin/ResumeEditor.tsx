// Resume editor. A structured form is the primary mode: basics fields plus
// add/edit/delete cards for experience, education, skills groups and projects.
// A validated raw-JSON mode is the fallback for everything else and for power
// edits; the two representations sync when you switch. Client-side checks give
// quick feedback, but the server's 400 is the authority. Save -> PUT
// /api/admin/resume with fileSha, sending the WHOLE doc, with the same
// conflict/GitHub/writes-disabled surfacing as the post editor.

import { useCallback, useEffect, useState } from 'react'

import {
  AdminApiError,
  AdminGitHubError,
  AdminStaleError,
  AdminUnauthorizedError,
  AdminWritesDisabledError,
  getResume,
  updateResume,
} from '../../lib/adminApi'
import { ResumeSkeleton } from '../../components/Skeletons'
import { useAdminAuth } from './AdminApp'
import {
  banner,
  btn,
  btnPrimary,
  errorBox,
  field,
  fieldLabel,
  input,
  linkButton,
  notice,
  subhead,
} from './ui'

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
interface ResumeExperienceDoc {
  org?: string
  role?: string
  start?: string
  end?: string | null
  location?: string
  highlights?: string[]
  [k: string]: unknown
}
interface ResumeEducationDoc {
  org?: string
  credential?: string
  start?: string
  end?: string
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

// Known structured sections, in canonical display/order. Missing ones get an
// "add" affordance in form mode so you don't have to drop to JSON to create them.
const SECTION_DEFAULTS: Record<string, () => ResumeSectionDoc> = {
  experience: () => ({ kind: 'experience', title: 'Experience', items: [] }),
  education: () => ({ kind: 'education', title: 'Education', items: [] }),
  skills: () => ({ kind: 'skills', title: 'Skills', groups: [] }),
  projects: () => ({ kind: 'projects', title: 'Projects', items: [] }),
}

// Drop blank / whitespace-only highlight lines so empty bullets never reach the
// public resume. The form keeps raw lines while editing (so Enter adds a line);
// we clean them here, on save, for both form and JSON modes.
function normalizeResume(doc: ResumeDoc): ResumeDoc {
  const sections = (doc.sections ?? []).map((section) => {
    if (!Array.isArray(section.items)) return section
    const items = section.items.map((item) => {
      if (
        item &&
        typeof item === 'object' &&
        Array.isArray((item as ResumeExperienceDoc).highlights)
      ) {
        const highlights = ((item as ResumeExperienceDoc).highlights ?? [])
          .map((h) => (typeof h === 'string' ? h.trim() : h))
          .filter((h) => (typeof h === 'string' ? h.length > 0 : h != null))
        return { ...(item as object), highlights }
      }
      return item
    })
    return { ...section, items }
  })
  return { ...doc, sections }
}

export default function ResumeEditor() {
  const { writesEnabled } = useAdminAuth()
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

  const basics = doc.basics ?? {}
  const sections = doc.sections ?? []
  const experienceIndex = sections.findIndex((s) => s.kind === 'experience')
  const educationIndex = sections.findIndex((s) => s.kind === 'education')
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

  const addSection = useCallback((kind: string) => {
    const make = SECTION_DEFAULTS[kind]
    if (!make) return
    setDoc((prev) => ({ ...prev, sections: [...(prev.sections ?? []), make()] }))
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

    // Strip blank highlight lines before validation / send.
    payload = normalizeResume(payload)

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
    return <ResumeSkeleton />
  }
  if (loadState === 'error') {
    return (
      <div role="alert">
        <p className="mb-1 font-semibold">Couldn’t load the resume</p>
        <p className="text-muted">{loadError}</p>
        <p className="mt-4">
          <button type="button" className={btn} onClick={() => load()}>
            Retry
          </button>
        </p>
      </div>
    )
  }

  return (
    <section className="max-w-3xl">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
        <h2 className="text-xl font-semibold">Resume</h2>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className={mode === 'form' ? btnPrimary : btn}
            onClick={() => switchMode('form')}
          >
            Form
          </button>
          <button
            type="button"
            className={mode === 'json' ? btnPrimary : btn}
            onClick={() => switchMode('json')}
          >
            JSON
          </button>
          <button
            type="button"
            className={btnPrimary}
            disabled={readOnly || saving}
            onClick={() => void onSave()}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {readOnly && (
        <p className={`mb-4 ${banner}`} role="status">
          This server is read-only (local content source).
        </p>
      )}
      {saveNotice && (
        <p className={`mb-4 ${notice}`} role="status">
          {saveNotice}
        </p>
      )}
      {saveError && (
        <p className={`mb-4 ${errorBox}`} role="alert">
          {saveError}
          {conflict && (
            <>
              {' '}
              <button type="button" className={linkButton} onClick={() => load()}>
                Reload
              </button>
            </>
          )}
        </p>
      )}

      {mode === 'json' ? (
        <textarea
          className={`${input} font-mono text-[13px] leading-relaxed`}
          value={jsonText}
          disabled={readOnly}
          spellCheck={false}
          rows={28}
          aria-label="Resume JSON"
          onChange={(e) => setJsonText(e.target.value)}
        />
      ) : (
        <div className="flex flex-col gap-8">
          <div>
            <h3 className={`mb-3 ${subhead}`}>Basics</h3>
            <div className="flex flex-col gap-4">
              <label className={field}>
                <span className={fieldLabel}>Name</span>
                <input
                  type="text"
                  className={input}
                  value={basics.name ?? ''}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('name', e.target.value)}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>Headline</span>
                <input
                  type="text"
                  className={input}
                  value={basics.headline ?? ''}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('headline', e.target.value)}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>Label</span>
                <input
                  type="text"
                  className={input}
                  value={basics.label ?? ''}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('label', e.target.value)}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>Email</span>
                <input
                  type="email"
                  className={input}
                  value={basics.email ?? ''}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('email', e.target.value)}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>Location</span>
                <input
                  type="text"
                  className={input}
                  value={basics.location ?? ''}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('location', e.target.value)}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>Summary</span>
                <textarea
                  className={input}
                  value={basics.summary ?? ''}
                  rows={3}
                  disabled={readOnly}
                  onChange={(e) => updateBasics('summary', e.target.value)}
                />
              </label>
            </div>
          </div>

          {experienceIndex >= 0 ? (
            <ExperienceEditor
              section={sections[experienceIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(experienceIndex, next)}
            />
          ) : (
            <AddSection title="Experience" readOnly={readOnly} onAdd={() => addSection('experience')} />
          )}
          {educationIndex >= 0 ? (
            <EducationEditor
              section={sections[educationIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(educationIndex, next)}
            />
          ) : (
            <AddSection title="Education" readOnly={readOnly} onAdd={() => addSection('education')} />
          )}
          {skillsIndex >= 0 ? (
            <SkillsEditor
              section={sections[skillsIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(skillsIndex, next)}
            />
          ) : (
            <AddSection title="Skills" readOnly={readOnly} onAdd={() => addSection('skills')} />
          )}
          {projectsIndex >= 0 ? (
            <ProjectsEditor
              section={sections[projectsIndex]}
              readOnly={readOnly}
              onChange={(next) => updateSection(projectsIndex, next)}
            />
          ) : (
            <AddSection title="Projects" readOnly={readOnly} onAdd={() => addSection('projects')} />
          )}

          <p className="text-sm text-muted">
            Any other sections are editable in JSON mode.
          </p>
        </div>
      )}
    </section>
  )
}

/* ---- Shared card chrome -------------------------------------------------- */

// Shown in place of a structured editor when that section isn't in the resume
// yet, so it can be created from the form instead of only in JSON mode.
function AddSection({
  title,
  readOnly,
  onAdd,
}: {
  title: string
  readOnly: boolean
  onAdd: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-3 border border-dashed border-line p-4">
      <div>
        <h3 className={subhead}>{title}</h3>
        <p className="text-sm text-muted">Not in this resume yet.</p>
      </div>
      <button type="button" className={btn} disabled={readOnly} onClick={onAdd}>
        Add {title.toLowerCase()}
      </button>
    </div>
  )
}

function SectionHeader({
  title,
  onAdd,
  readOnly,
  addLabel,
}: {
  title: string
  onAdd: () => void
  readOnly: boolean
  addLabel: string
}) {
  return (
    <div className="mb-3 flex items-center justify-between">
      <h3 className={subhead}>{title}</h3>
      <button type="button" className={btn} disabled={readOnly} onClick={onAdd}>
        {addLabel}
      </button>
    </div>
  )
}

function Card({
  children,
  onDelete,
  readOnly,
}: {
  children: React.ReactNode
  onDelete: () => void
  readOnly: boolean
}) {
  return (
    <div className="border border-line p-4">
      <div className="flex flex-col gap-3">{children}</div>
      <div className="mt-3 border-t border-line pt-3">
        <button type="button" className={btn} disabled={readOnly} onClick={onDelete}>
          Delete
        </button>
      </div>
    </div>
  )
}

/* ---- Experience ---------------------------------------------------------- */

function ExperienceEditor({
  section,
  readOnly,
  onChange,
}: {
  section: ResumeSectionDoc
  readOnly: boolean
  onChange: (next: ResumeSectionDoc) => void
}) {
  const items = (section.items ?? []) as ResumeExperienceDoc[]
  const setItem = (i: number, patch: Partial<ResumeExperienceDoc>) => {
    const next = [...items]
    next[i] = { ...next[i], ...patch }
    onChange({ ...section, items: next })
  }
  const addItem = () =>
    onChange({ ...section, items: [...items, { org: '', role: '', start: '', end: '', highlights: [] }] })
  const removeItem = (i: number) =>
    onChange({ ...section, items: items.filter((_, idx) => idx !== i) })

  return (
    <div>
      <SectionHeader
        title={section.title ?? 'Experience'}
        onAdd={addItem}
        readOnly={readOnly}
        addLabel="Add role"
      />
      <div className="flex flex-col gap-4">
        {items.map((item, i) => (
          <Card key={i} readOnly={readOnly} onDelete={() => removeItem(i)}>
            <label className={field}>
              <span className={fieldLabel}>Organisation</span>
              <input
                type="text"
                className={input}
                value={item.org ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { org: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Role</span>
              <input
                type="text"
                className={input}
                value={item.role ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { role: e.target.value })}
              />
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className={field}>
                <span className={fieldLabel}>Start</span>
                <input
                  type="text"
                  className={input}
                  value={item.start ?? ''}
                  disabled={readOnly}
                  placeholder="2023-10"
                  onChange={(e) => setItem(i, { start: e.target.value })}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>End</span>
                <input
                  type="text"
                  className={input}
                  value={item.end ?? ''}
                  disabled={readOnly}
                  placeholder="2025-08 or blank"
                  onChange={(e) => setItem(i, { end: e.target.value || null })}
                />
              </label>
            </div>
            <label className={field}>
              <span className={fieldLabel}>Location</span>
              <input
                type="text"
                className={input}
                value={item.location ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { location: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Highlights (one per line)</span>
              <textarea
                className={input}
                rows={4}
                value={(item.highlights ?? []).join('\n')}
                disabled={readOnly}
                onChange={(e) => setItem(i, { highlights: e.target.value.split('\n') })}
              />
            </label>
          </Card>
        ))}
      </div>
    </div>
  )
}

/* ---- Education ----------------------------------------------------------- */

function EducationEditor({
  section,
  readOnly,
  onChange,
}: {
  section: ResumeSectionDoc
  readOnly: boolean
  onChange: (next: ResumeSectionDoc) => void
}) {
  const items = (section.items ?? []) as ResumeEducationDoc[]
  const setItem = (i: number, patch: Partial<ResumeEducationDoc>) => {
    const next = [...items]
    next[i] = { ...next[i], ...patch }
    onChange({ ...section, items: next })
  }
  const addItem = () =>
    onChange({ ...section, items: [...items, { org: '', credential: '', start: '', end: '' }] })
  const removeItem = (i: number) =>
    onChange({ ...section, items: items.filter((_, idx) => idx !== i) })

  return (
    <div>
      <SectionHeader
        title={section.title ?? 'Education'}
        onAdd={addItem}
        readOnly={readOnly}
        addLabel="Add entry"
      />
      <div className="flex flex-col gap-4">
        {items.map((item, i) => (
          <Card key={i} readOnly={readOnly} onDelete={() => removeItem(i)}>
            <label className={field}>
              <span className={fieldLabel}>Organisation</span>
              <input
                type="text"
                className={input}
                value={item.org ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { org: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Credential</span>
              <input
                type="text"
                className={input}
                value={item.credential ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { credential: e.target.value })}
              />
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className={field}>
                <span className={fieldLabel}>Start</span>
                <input
                  type="text"
                  className={input}
                  value={item.start ?? ''}
                  disabled={readOnly}
                  onChange={(e) => setItem(i, { start: e.target.value })}
                />
              </label>
              <label className={field}>
                <span className={fieldLabel}>End</span>
                <input
                  type="text"
                  className={input}
                  value={item.end ?? ''}
                  disabled={readOnly}
                  onChange={(e) => setItem(i, { end: e.target.value })}
                />
              </label>
            </div>
          </Card>
        ))}
      </div>
    </div>
  )
}

/* ---- Skills -------------------------------------------------------------- */

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
  const addGroup = () =>
    onChange({ ...section, groups: [...groups, { name: '', items: [] }] })
  const removeGroup = (i: number) =>
    onChange({ ...section, groups: groups.filter((_, idx) => idx !== i) })

  return (
    <div>
      <SectionHeader
        title={section.title ?? 'Skills'}
        onAdd={addGroup}
        readOnly={readOnly}
        addLabel="Add group"
      />
      <div className="flex flex-col gap-4">
        {groups.map((group, i) => (
          <Card key={i} readOnly={readOnly} onDelete={() => removeGroup(i)}>
            <label className={field}>
              <span className={fieldLabel}>Group name</span>
              <input
                type="text"
                className={input}
                value={group.name ?? ''}
                disabled={readOnly}
                onChange={(e) => setGroup(i, { name: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Items (comma-separated)</span>
              <input
                type="text"
                className={input}
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
            </label>
          </Card>
        ))}
      </div>
    </div>
  )
}

/* ---- Projects ------------------------------------------------------------ */

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
  const addItem = () =>
    onChange({ ...section, items: [...items, { name: '', tag: '', url: '', description: '' }] })
  const removeItem = (i: number) =>
    onChange({ ...section, items: items.filter((_, idx) => idx !== i) })

  return (
    <div>
      <SectionHeader
        title={section.title ?? 'Projects'}
        onAdd={addItem}
        readOnly={readOnly}
        addLabel="Add project"
      />
      <div className="flex flex-col gap-4">
        {items.map((item, i) => (
          <Card key={i} readOnly={readOnly} onDelete={() => removeItem(i)}>
            <label className={field}>
              <span className={fieldLabel}>Name</span>
              <input
                type="text"
                className={input}
                value={item.name ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { name: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Tag</span>
              <input
                type="text"
                className={input}
                value={item.tag ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { tag: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>URL</span>
              <input
                type="text"
                className={input}
                value={item.url ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { url: e.target.value })}
              />
            </label>
            <label className={field}>
              <span className={fieldLabel}>Description</span>
              <textarea
                className={input}
                rows={2}
                value={item.description ?? ''}
                disabled={readOnly}
                onChange={(e) => setItem(i, { description: e.target.value })}
              />
            </label>
          </Card>
        ))}
      </div>
    </div>
  )
}
