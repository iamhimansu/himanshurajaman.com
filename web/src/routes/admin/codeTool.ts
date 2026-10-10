// Dedicated Editor.js code block tool storing { code, language }. It renders a
// language <select> + a <textarea> + a live highlight.js preview. The stored
// `language` drives the AUTHORITATIVE server-side chroma highlighting; the
// in-editor highlight.js preview is author feedback only (the two highlighters
// need not share classes). Language names below are chosen so BOTH chroma and
// highlight.js recognize them.

import type {
  BlockTool,
  BlockToolConstructorOptions,
  ToolboxConfig,
} from '@editorjs/editorjs'
import hljs from 'highlight.js/lib/common'
import 'highlight.js/styles/github.css'

export interface CodeToolData {
  code: string
  language: string
}

const LANGUAGES = [
  'plaintext', 'bash', 'c', 'cpp', 'csharp', 'css', 'go', 'html', 'java',
  'javascript', 'json', 'kotlin', 'markdown', 'php', 'python', 'ruby', 'rust',
  'sql', 'swift', 'typescript', 'yaml',
]

const CODE_ICON =
  '<svg width="17" height="15" viewBox="0 0 20 18" xmlns="http://www.w3.org/2000/svg"><path d="M6 4L2 9l4 5M14 4l4 5-4 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>'

export default class CodeTool implements BlockTool {
  private data: CodeToolData
  private readOnly: boolean
  private textarea: HTMLTextAreaElement | null = null
  private select: HTMLSelectElement | null = null

  static get isReadOnlySupported(): boolean {
    return true
  }

  static get enableLineBreaks(): boolean {
    return true
  }

  static get toolbox(): ToolboxConfig {
    return { title: 'Code', icon: CODE_ICON }
  }

  // Store code/language verbatim: strip any tags (there are none in a textarea
  // value), never interpret the content as HTML.
  static get sanitize() {
    return { code: false, language: false }
  }

  constructor({ data, readOnly }: BlockToolConstructorOptions<CodeToolData>) {
    this.readOnly = readOnly
    this.data = {
      code: typeof data?.code === 'string' ? data.code : '',
      language: typeof data?.language === 'string' && data.language ? data.language : 'plaintext',
    }
  }

  render(): HTMLElement {
    const wrapper = document.createElement('div')
    wrapper.className = 'cdx-code'

    const bar = document.createElement('div')
    bar.className = 'cdx-code__bar'
    const label = document.createElement('span')
    label.className = 'cdx-code__label'
    label.textContent = 'Code'
    const select = document.createElement('select')
    select.className = 'cdx-code__lang'
    select.disabled = this.readOnly
    for (const lang of LANGUAGES) {
      const opt = document.createElement('option')
      opt.value = lang
      opt.textContent = lang
      if (lang === this.data.language) opt.selected = true
      select.appendChild(opt)
    }
    bar.appendChild(label)
    bar.appendChild(select)

    const textarea = document.createElement('textarea')
    textarea.className = 'cdx-code__input'
    textarea.value = this.data.code
    textarea.placeholder = 'Enter code'
    textarea.spellcheck = false
    textarea.autocapitalize = 'off'
    textarea.setAttribute('autocomplete', 'off')
    textarea.setAttribute('autocorrect', 'off')
    textarea.readOnly = this.readOnly
    textarea.rows = Math.max(3, this.data.code.split('\n').length)

    const preview = document.createElement('pre')
    preview.className = 'cdx-code__preview'
    const codeEl = document.createElement('code')
    preview.appendChild(codeEl)

    const highlight = () => {
      const lang = select.value
      const src = textarea.value
      codeEl.className = ''
      if (lang && lang !== 'plaintext' && hljs.getLanguage(lang)) {
        try {
          codeEl.innerHTML = hljs.highlight(src, { language: lang }).value
          codeEl.className = `hljs language-${lang}`
          return
        } catch {
          /* fall through to plain text */
        }
      }
      codeEl.textContent = src
      codeEl.className = 'hljs'
    }

    const sync = () => {
      this.data.code = textarea.value
      this.data.language = select.value
      textarea.rows = Math.max(3, textarea.value.split('\n').length)
      highlight()
    }

    if (!this.readOnly) {
      textarea.addEventListener('input', sync)
      select.addEventListener('change', sync)
      // Keep tabs inside the code block instead of moving focus.
      textarea.addEventListener('keydown', (e) => {
        if (e.key === 'Tab') {
          e.preventDefault()
          const start = textarea.selectionStart
          const end = textarea.selectionEnd
          textarea.value = textarea.value.slice(0, start) + '  ' + textarea.value.slice(end)
          textarea.selectionStart = textarea.selectionEnd = start + 2
          sync()
        }
      })
    }
    highlight()

    wrapper.appendChild(bar)
    wrapper.appendChild(textarea)
    wrapper.appendChild(preview)
    this.textarea = textarea
    this.select = select
    return wrapper
  }

  save(): CodeToolData {
    return {
      code: this.textarea ? this.textarea.value : this.data.code,
      language: this.select ? this.select.value : this.data.language,
    }
  }

  validate(data: CodeToolData): boolean {
    return typeof data.code === 'string' && data.code.trim().length > 0
  }
}
