// Editor.js setup, isolated in its own module so EditorBody can pull it in via a
// DYNAMIC import(). That keeps Editor.js and all its tools (plus highlight.js,
// imported by the code tool) in a SEPARATE chunk, loaded only when the editor
// actually mounts — the visitor entry bundle never sees any of it.

import EditorJS, {
  type EditorConfig,
  type OutputData,
  type ToolConstructable,
} from '@editorjs/editorjs'
import Header from '@editorjs/header'
import EditorjsList from '@editorjs/list'
import Quote from '@editorjs/quote'
import Table from '@editorjs/table'
import ImageTool from '@editorjs/image'
import Delimiter from '@editorjs/delimiter'
import InlineCode from '@editorjs/inline-code'
import Marker from '@editorjs/marker'

import CodeTool from './codeTool'

export interface CreateEditorOptions {
  holder: HTMLElement
  data?: OutputData
  readOnly: boolean
  /** Upload one image and resolve to its served URL (POST /api/admin/images). */
  uploadImage: (file: File) => Promise<{ url: string }>
}

export function createEditor(opts: CreateEditorOptions): EditorJS {
  const tools: EditorConfig['tools'] = {
    header: {
      class: Header,
      inlineToolbar: true,
      config: { levels: [2, 3, 4], defaultLevel: 2, placeholder: 'Heading' },
    },
    list: {
      class: EditorjsList,
      inlineToolbar: true,
      config: { defaultStyle: 'unordered' },
    },
    quote: { class: Quote, inlineToolbar: true },
    // The table tool's .d.ts types its constructor's `config` as required,
    // which does not match Editor.js's optional BlockToolConstructorOptions;
    // cast to the expected constructable (runtime behavior is unaffected).
    table: { class: Table as unknown as ToolConstructable, inlineToolbar: true },
    code: CodeTool,
    image: {
      class: ImageTool,
      config: {
        uploader: {
          uploadByFile: (file: Blob) =>
            opts.uploadImage(file as File).then((res) => ({
              success: 1,
              file: { url: res.url },
            })),
        },
      },
    },
    delimiter: Delimiter,
    marker: Marker,
    inlineCode: InlineCode,
  }

  return new EditorJS({
    holder: opts.holder,
    data: opts.data,
    readOnly: opts.readOnly,
    minHeight: 240,
    placeholder: 'Write your post… press Tab or “+” to add a block',
    tools,
  })
}

export type { OutputData }
export type EditorInstance = EditorJS
