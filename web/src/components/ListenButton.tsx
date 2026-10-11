// Read-aloud control for the post reader (A4). Uses the native Web Speech API
// (window.speechSynthesis) — no npm dependency, no server call. Client-only:
// renders nothing when speech synthesis is unavailable (SSR / unsupported
// browsers).
//
// Word sync: while speaking, the word currently being read is highlighted in
// the article, driven by the utterance's `boundary` events. To map a boundary's
// charIndex back to the page, each word in the article is wrapped in a
// <span class="tts-word"> at playback and the spoken string is built from the
// SAME walk, so the engine's charIndex lines up with each span's recorded
// [start,end) offset. The spans are unwrapped again when playback stops/ends.
//
// This component lives in the PUBLIC chunk only — it imports nothing from
// routes/admin or the editor chunk, so it never pulls admin code into the
// visitor entry bundle.

import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'

type SpeechState = 'idle' | 'speaking' | 'paused'

interface ListenButtonProps {
  targetRef: React.RefObject<HTMLElement | null>
}

interface WordSpan {
  el: HTMLElement
  start: number
  end: number
}

const WORD_CLASS = 'tts-word'
const ACTIVE_CLASS = 'tts-active'
// Don't wrap (or speak) code blocks / scripts — reading source aloud word by
// word is noise, and wrapping would disturb the chroma highlight spans.
const SKIP_TAGS = new Set(['PRE', 'CODE', 'SCRIPT', 'STYLE'])

function speechAvailable(): boolean {
  return typeof window !== 'undefined' && 'speechSynthesis' in window
}

// Wrap every word under `root` in a <span class="tts-word"> and build the exact
// string to speak, recording each word's [start,end) offset into that string.
// Runs on a clean (unwrapped) subtree; callers unwrap before re-running.
function wrapWords(root: HTMLElement): { words: WordSpan[]; text: string } {
  const words: WordSpan[] = []
  let text = ''

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (!node.nodeValue || node.nodeValue.trim() === '') {
        return NodeFilter.FILTER_REJECT
      }
      for (let p = node.parentElement; p && p !== root; p = p.parentElement) {
        if (SKIP_TAGS.has(p.tagName)) return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    },
  })

  // Snapshot first: we replace nodes as we go.
  const textNodes: Text[] = []
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    textNodes.push(n as Text)
  }

  for (const node of textNodes) {
    const value = node.nodeValue ?? ''
    // Keep spoken words from adjacent block text nodes from running together.
    if (text.length > 0 && !/\s$/.test(text)) text += ' '

    const frag = document.createDocumentFragment()
    for (const part of value.split(/(\s+)/)) {
      if (part === '') continue
      if (/^\s+$/.test(part)) {
        text += part
        frag.appendChild(document.createTextNode(part))
      } else {
        const start = text.length
        text += part
        const span = document.createElement('span')
        span.className = WORD_CLASS
        span.textContent = part
        frag.appendChild(span)
        words.push({ el: span, start, end: text.length })
      }
    }
    node.parentNode?.replaceChild(frag, node)
  }

  return { words, text }
}

// Restore the original DOM: replace each wrapper span with its text and merge
// adjacent text nodes so a later wrap sees a clean subtree.
function unwrapWords(root: HTMLElement | null): void {
  if (!root) return
  const spans = root.querySelectorAll<HTMLElement>('.' + WORD_CLASS)
  spans.forEach((span) => {
    const parent = span.parentNode
    if (!parent) return
    while (span.firstChild) parent.insertBefore(span.firstChild, span)
    parent.removeChild(span)
  })
  if (spans.length > 0) root.normalize()
}

export default function ListenButton({ targetRef }: ListenButtonProps) {
  const [state, setState] = useState<SpeechState>('idle')
  const { pathname } = useLocation()

  const wordsRef = useRef<WordSpan[]>([])
  const activeRef = useRef<number>(-1)

  const clearHighlight = useCallback(() => {
    const words = wordsRef.current
    if (activeRef.current >= 0) {
      words[activeRef.current]?.el.classList.remove(ACTIVE_CLASS)
    }
    activeRef.current = -1
  }, [])

  // Full teardown: drop the highlight, remove the wrapper spans, forget the map.
  const teardown = useCallback(() => {
    clearHighlight()
    unwrapWords(targetRef.current)
    wordsRef.current = []
  }, [clearHighlight, targetRef])

  // Stop any in-flight speech and reset on unmount and whenever the route
  // changes, so audio and highlights never bleed across posts.
  useEffect(() => {
    return () => {
      if (speechAvailable()) window.speechSynthesis.cancel()
      activeRef.current = -1
      wordsRef.current = []
      setState('idle')
    }
  }, [pathname])

  const highlightAt = useCallback((charIndex: number) => {
    const words = wordsRef.current
    if (words.length === 0) return
    // First word whose end offset is past charIndex (handles boundaries that
    // land in the whitespace just before a word).
    let lo = 0
    let hi = words.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (words[mid].end <= charIndex) lo = mid + 1
      else hi = mid
    }
    if (lo >= words.length || lo === activeRef.current) return
    if (activeRef.current >= 0) {
      words[activeRef.current]?.el.classList.remove(ACTIVE_CLASS)
    }
    words[lo].el.classList.add(ACTIVE_CLASS)
    activeRef.current = lo
  }, [])

  const handleToggle = useCallback(() => {
    if (!speechAvailable()) return
    const synth = window.speechSynthesis

    if (state === 'speaking') {
      synth.pause()
      setState('paused')
      return
    }
    if (state === 'paused') {
      synth.resume()
      setState('speaking')
      return
    }

    const container = targetRef.current
    if (!container) return

    // Fresh start: clean any stale spans, then wrap + build the spoken string.
    unwrapWords(container)
    const { words, text } = wrapWords(container)
    if (text.trim() === '') {
      unwrapWords(container)
      return
    }
    wordsRef.current = words
    activeRef.current = -1

    synth.cancel()
    const utterance = new SpeechSynthesisUtterance(text)
    utterance.onboundary = (e) => {
      if (e.name && e.name !== 'word') return
      highlightAt(e.charIndex)
    }
    utterance.onend = () => {
      teardown()
      setState('idle')
    }
    utterance.onerror = () => {
      teardown()
      setState('idle')
    }
    synth.speak(utterance)
    setState('speaking')
  }, [state, targetRef, highlightAt, teardown])

  const handleStop = useCallback(() => {
    if (!speechAvailable()) return
    window.speechSynthesis.cancel()
    teardown()
    setState('idle')
  }, [teardown])

  // Render nothing where the API is unavailable (keeps the server/SSR output
  // and unsupported browsers clean).
  if (!speechAvailable()) return null

  const label =
    state === 'speaking' ? 'Pause' : state === 'paused' ? 'Resume' : 'Listen'

  return (
    <span className="inline-flex items-center gap-2">
      <button
        type="button"
        onClick={handleToggle}
        aria-pressed={state === 'speaking'}
        className="inline-flex items-center gap-1.5 border border-line bg-surface px-3 py-1 text-sm font-semibold text-text hover:border-line-strong"
      >
        <span aria-hidden="true" className="text-accent">
          {state === 'speaking' ? '❚❚' : '▶'}
        </span>
        {label}
      </button>
      {state !== 'idle' && (
        <button
          type="button"
          onClick={handleStop}
          className="border border-line bg-surface px-3 py-1 text-sm text-muted hover:border-line-strong hover:text-text"
        >
          Stop
        </button>
      )}
    </span>
  )
}
