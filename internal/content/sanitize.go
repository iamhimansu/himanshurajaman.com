package content

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// newSanitizePolicy starts from bluemonday's UGC policy (a safe subset) and
// extends it to preserve the markup the server-side Editor.js renderer emits:
// <pre class="chroma"><code> wrapping <span class="..."> highlight tokens,
// heading anchor ids, figure/figcaption image captions, and the <mark>/<cite>
// inline elements. Without allowing `class` on these elements, highlighting is
// stripped. The policy is the last line of defense: the author is the admin,
// but every rendered document still passes through it (defense in depth).
func newSanitizePolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowTables()
	// Figure/caption (images) and inline highlight/citation elements the UGC
	// policy does not allow by default.
	p.AllowElements("figure", "figcaption", "mark", "cite")
	// Highlighting + heading anchors.
	p.AllowAttrs("class").OnElements("pre", "code", "span", "div")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	// Allow lazy-loading hints and explicit dimensions on images (used by the
	// frontend in Phase 3); src/alt are already permitted by the UGC policy.
	p.AllowAttrs("loading", "width", "height").OnElements("img")
	return p
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
