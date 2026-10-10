// @editorjs/marker ships no type declarations; declare its default export as a
// tool constructable so it slots into the Editor.js tools map.
declare module '@editorjs/marker' {
  import type { ToolConstructable } from '@editorjs/editorjs'
  const Marker: ToolConstructable
  export default Marker
}
