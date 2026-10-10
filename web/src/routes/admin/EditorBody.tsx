// Editor.js body. It mounts the editor via a DYNAMIC import('./editor') so the
// heavy Editor.js runtime + tools (+ highlight.js) load only when the editor is
// shown, and stay in a separate chunk the visitor bundle never imports. The
// parent uses the forwarded ref to serialize the document on save. To reset the
// contents (e.g. after a conflict reload) remount this with a new `key`.

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from 'react'

import type { EditorInstance, OutputData } from './editor'

export interface EditorBodyHandle {
  save: () => Promise<OutputData>
}

interface EditorBodyProps {
  initialData?: OutputData
  readOnly: boolean
  uploadImage: (file: File) => Promise<{ url: string }>
}

const EditorBody = forwardRef<EditorBodyHandle, EditorBodyProps>(function EditorBody(
  { initialData, readOnly, uploadImage },
  ref,
) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const editorRef = useRef<EditorInstance | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)

  // Keep the latest uploadImage without re-creating the editor.
  const uploadRef = useRef(uploadImage)
  uploadRef.current = uploadImage

  useImperativeHandle(ref, () => ({
    async save() {
      const ed = editorRef.current
      if (!ed) throw new Error('Editor is not ready yet')
      return ed.save()
    },
  }))

  useEffect(() => {
    let cancelled = false

    // DYNAMIC import: this is what keeps Editor.js in its own chunk.
    import('./editor')
      .then(({ createEditor }) => {
        if (cancelled || !hostRef.current) return
        const editor = createEditor({
          holder: hostRef.current,
          data: initialData,
          readOnly,
          uploadImage: (f) => uploadRef.current(f),
        })
        editorRef.current = editor
        return editor.isReady
          .then(() => {
            if (!cancelled) setLoaded(true)
          })
          .catch(() => {
            if (!cancelled) setFailed(true)
          })
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })

    return () => {
      cancelled = true
      const ed = editorRef.current
      editorRef.current = null
      if (ed) {
        // Destroy only after it finished initializing, to avoid a teardown race.
        ed.isReady.then(() => ed.destroy()).catch(() => {})
      }
    }
    // initialData/readOnly are read on mount only; remount via `key` to reset.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="admin-editor-host">
      <div ref={hostRef} className="admin-editorjs" />
      {!loaded && !failed && (
        <p className="muted admin-editor-loading">Loading editor…</p>
      )}
      {failed && (
        <p className="admin-error" role="alert">
          The editor failed to load. Reload the page to try again.
        </p>
      )}
    </div>
  )
})

export default EditorBody
