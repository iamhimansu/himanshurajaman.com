// AI draft panel for the New-post flow. The author types a short brief (English
// or Hindi), optionally picks a language and ONE OR MORE models, and clicks
// Generate. The backend (POST /api/admin/assist) calls Groq once per model in
// parallel and returns one candidate draft per model; this renders every
// candidate side by side and lets the author apply one with "Use this draft".
// Applying fills the form + loads the Editor.js blocks — it never auto-saves.
//
// Lives in the admin lazy chunk (imported only from PostEditor). It uses fetch
// via adminApi and renders a plain-text preview of each candidate, so it never
// pulls in Editor.js.

import { useCallback, useEffect, useRef, useState } from 'react'

import {
  AdminUnauthorizedError,
  AiAllFailedError,
  AiBadRequestError,
  AiUnavailableError,
  assistDraft,
  listAiModels,
} from '../../lib/adminApi'
import type { AiCandidate, AiLanguage, AiModelError } from '../../lib/types'
import {
  btn,
  btnPrimary,
  errorBox,
  field,
  fieldLabel,
  input,
  notice,
  panel,
  subhead,
  tagChip,
} from './ui'

interface AiDraftPanelProps {
  /** Apply a chosen candidate to the form + editor. Never auto-saves. */
  onApply: (candidate: AiCandidate) => void
}

/** Strip the limited inline HTML the model may emit so previews read as text. */
function stripInline(s: string): string {
  return s
    .replace(/<[^>]*>/g, '')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&nbsp;/g, ' ')
}

function asStr(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

const LANGUAGES: { value: AiLanguage; label: string }[] = [
  { value: 'auto', label: 'Auto (match the brief)' },
  { value: 'en', label: 'English' },
  { value: 'hi', label: 'Hindi' },
]

const PREVIEW_BLOCK_LIMIT = 8

export default function AiDraftPanel({ onApply }: AiDraftPanelProps) {
  const [brief, setBrief] = useState('')
  const [language, setLanguage] = useState<AiLanguage>('auto')

  const [models, setModels] = useState<string[]>([])
  const [maxModels, setMaxModels] = useState(1)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [modelsState, setModelsState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [modelsError, setModelsError] = useState('')

  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState('')
  const [candidates, setCandidates] = useState<AiCandidate[]>([])
  const [modelErrors, setModelErrors] = useState<AiModelError[]>([])
  const [appliedModel, setAppliedModel] = useState<string | null>(null)

  // Load the model allowlist once.
  useEffect(() => {
    const controller = new AbortController()
    setModelsState('loading')
    listAiModels(controller.signal)
      .then((res) => {
        if (controller.signal.aborted) return
        setModels(res.models)
        setMaxModels(Math.max(1, res.max || 1))
        const initial =
          res.default && res.models.includes(res.default) ? res.default : res.models[0]
        setSelected(new Set(initial ? [initial] : []))
        setModelsState('ready')
      })
      .catch((err) => {
        if (controller.signal.aborted) return
        if (err instanceof AdminUnauthorizedError) return
        setModelsError(
          err instanceof AiUnavailableError
            ? 'AI drafting is not available right now.'
            : err instanceof Error
              ? err.message
              : 'Could not load models.',
        )
        setModelsState('error')
      })
    return () => controller.abort()
  }, [])

  const toggleModel = useCallback(
    (model: string) => {
      setSelected((prev) => {
        const next = new Set(prev)
        if (next.has(model)) {
          next.delete(model)
        } else if (next.size < maxModels) {
          next.add(model)
        }
        return next
      })
    },
    [maxModels],
  )

  const genController = useRef<AbortController | null>(null)
  useEffect(() => () => genController.current?.abort(), [])

  const canGenerate = brief.trim().length > 0 && selected.size > 0 && !generating

  const onGenerate = useCallback(async () => {
    if (!canGenerate) return
    genController.current?.abort()
    const controller = new AbortController()
    genController.current = controller

    setGenerating(true)
    setGenError('')
    setCandidates([])
    setModelErrors([])
    setAppliedModel(null)
    try {
      const res = await assistDraft(
        { input: brief.trim(), language, models: [...selected] },
        controller.signal,
      )
      if (controller.signal.aborted) return
      setCandidates(res.candidates)
      setModelErrors(res.errors)
    } catch (err) {
      if (controller.signal.aborted) return
      if (err instanceof AiAllFailedError) {
        setModelErrors(err.errors)
        setGenError('Every selected model failed. See the notes below.')
      } else if (err instanceof AiUnavailableError) {
        setGenError('AI drafting is not available right now.')
      } else if (err instanceof AiBadRequestError) {
        setGenError(err.message)
      } else if (!(err instanceof AdminUnauthorizedError)) {
        setGenError(err instanceof Error ? err.message : 'Generation failed.')
      }
    } finally {
      if (!controller.signal.aborted) setGenerating(false)
    }
  }, [brief, language, selected, canGenerate])

  const onUse = useCallback(
    (candidate: AiCandidate) => {
      setAppliedModel(candidate.model)
      onApply(candidate)
    },
    [onApply],
  )

  return (
    <section className={`${panel} mb-6 p-4`} aria-labelledby="ai-draft-heading">
      <div className="flex items-center justify-between gap-3">
        <h3 id="ai-draft-heading" className={subhead}>
          Draft with AI
        </h3>
        <span className="font-mono text-[11px] uppercase tracking-[0.06em] text-muted">
          Groq
        </span>
      </div>
      <p className="mt-1 text-sm text-muted">
        Write a rough brief (English or Hindi). The selected models each return a
        full draft; apply the one you like, then review and save as usual.
      </p>

      <div className="mt-4 flex flex-col gap-4">
        <label className={field}>
          <span className={fieldLabel}>Brief</span>
          <textarea
            className={input}
            value={brief}
            rows={4}
            maxLength={4000}
            disabled={generating}
            placeholder="e.g. A short post on why we moved from REST to gRPC, with a small Go example and a trade-offs list."
            onChange={(e) => setBrief(e.target.value)}
          />
          <small className="text-muted">{brief.length}/4000</small>
        </label>

        <label className={`${field} sm:max-w-xs`}>
          <span className={fieldLabel}>Language</span>
          <select
            className={input}
            value={language}
            disabled={generating}
            onChange={(e) => setLanguage(e.target.value as AiLanguage)}
          >
            {LANGUAGES.map((l) => (
              <option key={l.value} value={l.value}>
                {l.label}
              </option>
            ))}
          </select>
        </label>

        <div className={field}>
          <span className={fieldLabel}>
            Models{' '}
            <span className="font-normal normal-case">
              (pick up to {maxModels}; {selected.size} selected)
            </span>
          </span>
          {modelsState === 'loading' && <p className="text-sm text-muted">Loading models…</p>}
          {modelsState === 'error' && (
            <p className={errorBox} role="alert">
              {modelsError}
            </p>
          )}
          {modelsState === 'ready' && (
            <div className="flex flex-col gap-1.5 border border-line p-2">
              {models.map((model) => {
                const checked = selected.has(model)
                const atCap = !checked && selected.size >= maxModels
                return (
                  <label
                    key={model}
                    className={`flex items-center gap-2 text-sm ${atCap ? 'text-muted' : 'text-text'}`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={generating || atCap}
                      onChange={() => toggleModel(model)}
                    />
                    <span className="font-mono text-[13px]">{model}</span>
                  </label>
                )
              })}
            </div>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className={btnPrimary}
            disabled={!canGenerate || modelsState !== 'ready'}
            onClick={() => void onGenerate()}
          >
            {generating ? 'Generating…' : 'Generate'}
          </button>
          {generating && (
            <span className="text-sm text-muted" role="status" aria-live="polite">
              Asking {selected.size} model{selected.size === 1 ? '' : 's'}…
            </span>
          )}
        </div>

        {genError && (
          <p className={errorBox} role="alert">
            {genError}
          </p>
        )}

        {modelErrors.length > 0 && (
          <ul className="flex flex-col gap-1" aria-label="Model errors">
            {modelErrors.map((e) => (
              <li key={e.model} className="text-sm text-muted">
                <span className="font-mono text-[13px]">{e.model}</span>: {e.message}
              </li>
            ))}
          </ul>
        )}

        {candidates.length > 0 && (
          <div className="flex flex-col gap-4">
            <p className={subhead}>
              {candidates.length} draft{candidates.length === 1 ? '' : 's'}
            </p>
            <div className="grid gap-4 lg:grid-cols-2">
              {candidates.map((candidate) => (
                <CandidateCard
                  key={candidate.model}
                  candidate={candidate}
                  applied={appliedModel === candidate.model}
                  onUse={() => onUse(candidate)}
                />
              ))}
            </div>
          </div>
        )}

        {appliedModel && (
          <p className={notice} role="status">
            Applied the draft from <span className="font-mono">{appliedModel}</span>. Review the
            fields and content below, then Save.
          </p>
        )}
      </div>
    </section>
  )
}

function CandidateCard({
  candidate,
  applied,
  onUse,
}: {
  candidate: AiCandidate
  applied: boolean
  onUse: () => void
}) {
  const blocks = candidate.content?.blocks ?? []
  const shown = blocks.slice(0, PREVIEW_BLOCK_LIMIT)
  const extra = blocks.length - shown.length

  return (
    <article className={`${applied ? 'border-text' : 'border-line'} flex flex-col border bg-bg p-3`}>
      <div className="flex items-center justify-between gap-2 border-b border-line pb-2">
        <span className="font-mono text-[12px] text-muted">{candidate.model}</span>
        {applied && (
          <span className="font-mono text-[11px] uppercase tracking-[0.04em] text-text">
            Applied
          </span>
        )}
      </div>

      <h4 className="mt-2 font-semibold text-text">{candidate.title || '(untitled)'}</h4>
      {candidate.summary && <p className="mt-1 text-sm text-muted">{candidate.summary}</p>}

      {candidate.tags.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {candidate.tags.map((tag) => (
            <span key={tag} className={tagChip}>
              {tag}
            </span>
          ))}
        </div>
      )}

      {candidate.slug && (
        <p className="mt-2 font-mono text-[12px] text-muted">/{candidate.slug}</p>
      )}

      <div className="mt-3 max-h-72 overflow-y-auto border-t border-line pt-2 text-sm">
        {shown.map((block, i) => (
          <BlockPreview key={i} type={block.type} data={block.data as Record<string, unknown>} />
        ))}
        {extra > 0 && (
          <p className="mt-2 text-xs text-muted">
            +{extra} more block{extra === 1 ? '' : 's'}
          </p>
        )}
      </div>

      <div className="mt-3">
        <button type="button" className={`${btn} w-full`} onClick={onUse}>
          Use this draft
        </button>
      </div>
    </article>
  )
}

function BlockPreview({ type, data }: { type: string; data: Record<string, unknown> }) {
  switch (type) {
    case 'header':
      return <p className="mt-2 font-semibold text-text">{stripInline(asStr(data.text))}</p>
    case 'paragraph':
      return <p className="mt-1 text-muted">{stripInline(asStr(data.text))}</p>
    case 'quote':
      return (
        <p className="mt-1 border-l-2 border-line pl-2 italic text-muted">
          {stripInline(asStr(data.text))}
        </p>
      )
    case 'code':
      return (
        <pre className="mt-1 overflow-x-auto bg-raise p-2 font-mono text-[12px] text-text">
          {asStr(data.code)}
        </pre>
      )
    case 'delimiter':
      return <hr className="my-2 border-line" />
    case 'list': {
      const items = Array.isArray(data.items) ? data.items : []
      const ordered = data.style === 'ordered'
      const Tag = ordered ? 'ol' : 'ul'
      return (
        <Tag className={`mt-1 pl-5 text-muted ${ordered ? 'list-decimal' : 'list-disc'}`}>
          {items.slice(0, 6).map((it, i) => {
            const text = typeof it === 'string' ? it : asStr((it as Record<string, unknown>)?.content)
            return <li key={i}>{stripInline(text)}</li>
          })}
        </Tag>
      )
    }
    case 'table': {
      const rows = Array.isArray(data.content) ? (data.content as unknown[]) : []
      return (
        <p className="mt-1 text-muted">
          Table ({rows.length} row{rows.length === 1 ? '' : 's'})
        </p>
      )
    }
    default:
      return null
  }
}
