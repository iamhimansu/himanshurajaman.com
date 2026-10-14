package server

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
)

// Compiled-in fallbacks used when settings are missing or the cache is still
// warming. The description mirrors the shell's static <meta name="description">.
const (
	defaultSiteTitle       = "Himanshu Raj Aman"
	defaultSiteDescription = "Personal portfolio, resume, and a blog about bugs, research, and benchmarks."
)

// Shell-splitting patterns. The static <title> and <meta name="description">
// are stripped from the head so renderMeta can inject exactly one of each.
var (
	reShellTitle = regexp.MustCompile(`(?is)\s*<title>.*?</title>`)
	reShellDesc  = regexp.MustCompile(`(?is)\s*<meta\s+name=["']description["'][^>]*>`)
)

// splitShell splits the embedded index.html around </head>. head is everything
// before </head> (with the static title/description stripped) and tail is
// </head> onward. When </head> is absent it returns (index, nil) to signal a
// failed split so the caller falls back to the raw shell.
func splitShell(index []byte) (head, tail []byte) {
	i := strings.Index(strings.ToLower(string(index)), "</head>")
	if i < 0 {
		return index, nil
	}
	h := index[:i]
	h = reShellTitle.ReplaceAll(h, nil)
	h = reShellDesc.ReplaceAll(h, nil)
	return h, index[i:]
}

// absURL resolves ref against base. A ref that is already absolute (http/https)
// is returned unchanged; an empty base returns ref unchanged; otherwise base
// (trailing slash trimmed) is joined with ref honoring a leading '/'.
func absURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if base == "" {
		return ref
	}
	base = strings.TrimRight(base, "/")
	if strings.HasPrefix(ref, "/") {
		return base + ref
	}
	return base + "/" + ref
}

// siteSettings is the resolved set of site-wide meta values. It is seeded from
// configBaseURL + compiled-in defaults, then overlaid with any present string
// keys from the settings cache.
type siteSettings struct {
	BaseURL        string
	Title          string
	Description    string
	DefaultOGImage string
	Author         string
}

// siteSettings builds the resolved settings for meta generation. It is
// warming-safe: on a store error or empty settings map it keeps the fallbacks.
func (s *Server) siteSettings(ctx context.Context) siteSettings {
	ss := siteSettings{
		BaseURL:     strings.TrimRight(s.configBaseURL, "/"),
		Title:       defaultSiteTitle,
		Description: defaultSiteDescription,
	}

	raw, err := s.store.Settings(ctx)
	if err != nil || len(raw) == 0 {
		return ss
	}

	overlay := func(key string, dst *string) {
		v, ok := raw[key]
		if !ok {
			return
		}
		var str string
		if json.Unmarshal(v, &str) == nil && str != "" {
			*dst = str
		}
	}
	overlay("baseUrl", &ss.BaseURL)
	overlay("title", &ss.Title)
	overlay("description", &ss.Description)
	overlay("defaultOgImage", &ss.DefaultOGImage)
	overlay("author", &ss.Author)

	ss.BaseURL = strings.TrimRight(ss.BaseURL, "/")
	return ss
}

// docMeta is the per-document set of head values renderMeta emits.
type docMeta struct {
	Title            string
	Description      string
	Canonical        string
	OGType           string
	OGURL            string
	OGImage          string
	SiteName         string
	ArticlePublished string
	ArticleModified  string
}

// renderMeta builds the <head> meta fragment for m. Every value is
// HTML-escaped. Empty optional values (og:image, article times) are omitted.
func renderMeta(m docMeta) string {
	var b strings.Builder
	esc := html.EscapeString

	b.WriteString("<title>" + esc(m.Title) + "</title>\n")
	b.WriteString(`<meta name="description" content="` + esc(m.Description) + `">` + "\n")
	b.WriteString(`<link rel="canonical" href="` + esc(m.Canonical) + `">` + "\n")
	b.WriteString(`<meta property="og:title" content="` + esc(m.Title) + `">` + "\n")
	b.WriteString(`<meta property="og:description" content="` + esc(m.Description) + `">` + "\n")
	b.WriteString(`<meta property="og:type" content="` + esc(m.OGType) + `">` + "\n")
	b.WriteString(`<meta property="og:url" content="` + esc(m.OGURL) + `">` + "\n")
	b.WriteString(`<meta property="og:site_name" content="` + esc(m.SiteName) + `">` + "\n")
	if m.OGImage != "" {
		b.WriteString(`<meta property="og:image" content="` + esc(m.OGImage) + `">` + "\n")
	}
	b.WriteString(`<meta name="twitter:card" content="summary_large_image">` + "\n")
	if m.OGType == "article" {
		if m.ArticlePublished != "" {
			b.WriteString(`<meta property="article:published_time" content="` + esc(m.ArticlePublished) + `">` + "\n")
		}
		if m.ArticleModified != "" {
			b.WriteString(`<meta property="article:modified_time" content="` + esc(m.ArticleModified) + `">` + "\n")
		}
	}
	return b.String()
}

// resolveDocMeta produces the rendered meta fragment and HTTP status for a
// public document route. It reads ONLY the cache (settings + GetPost) and never
// calls GitHub. When the server is still warming it short-circuits to default
// meta with status 200 for every path (no cache lookup).
func (s *Server) resolveDocMeta(ctx context.Context, r *http.Request) (meta string, status int) {
	ss := s.siteSettings(ctx)
	path := r.URL.Path

	// Default (website) meta for a path.
	defaultMeta := func(p string) docMeta {
		return docMeta{
			Title:       ss.Title,
			Description: ss.Description,
			Canonical:   absURL(ss.BaseURL, p),
			OGType:      "website",
			OGURL:       absURL(ss.BaseURL, p),
			OGImage:     absURL(ss.BaseURL, ss.DefaultOGImage),
			SiteName:    ss.Title,
		}
	}

	ready, _ := s.readyState()
	if !ready {
		return renderMeta(defaultMeta(path)), http.StatusOK
	}

	switch {
	case path == "/" || path == "/blog" || path == "/resume" || path == "/search" ||
		path == "/works" || path == "/contact" ||
		path == "/admin" || strings.HasPrefix(path, "/admin/"):
		return renderMeta(defaultMeta(path)), http.StatusOK

	case strings.HasPrefix(path, "/blog/"):
		slug := strings.TrimPrefix(path, "/blog/")
		if slug == "" || strings.Contains(slug, "/") {
			return renderMeta(defaultMeta(path)), http.StatusNotFound
		}
		post, err := s.store.GetPost(ctx, slug)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return renderMeta(defaultMeta(path)), http.StatusNotFound
			}
			s.log.Error("resolve doc meta", "slug", slug, "error", err.Error())
			return renderMeta(defaultMeta(path)), http.StatusOK
		}

		ogImage := ss.DefaultOGImage
		if post.CoverImage != nil && *post.CoverImage != "" {
			ogImage = *post.CoverImage
		}
		m := docMeta{
			Title:            post.Title + " — " + ss.Title,
			Description:      post.Summary,
			Canonical:        absURL(ss.BaseURL, "/blog/"+slug),
			OGType:           "article",
			OGURL:            absURL(ss.BaseURL, "/blog/"+slug),
			OGImage:          absURL(ss.BaseURL, ogImage),
			SiteName:         ss.Title,
			ArticlePublished: post.PublishedAt,
		}
		if post.UpdatedAt != nil {
			m.ArticleModified = *post.UpdatedAt
		}
		return renderMeta(m), http.StatusOK

	default:
		// A top-level /:pageSlug may be a published page. A nested path, an
		// empty slug, or a reserved word can never be a page: default + 404.
		slug := strings.TrimPrefix(path, "/")
		if slug == "" || strings.Contains(slug, "/") || content.IsReservedSlug(slug) {
			return renderMeta(defaultMeta(path)), http.StatusNotFound
		}
		page, err := s.store.GetPage(ctx, slug)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.log.Error("resolve doc meta page", "slug", slug, "error", err.Error())
			}
			return renderMeta(defaultMeta(path)), http.StatusNotFound
		}

		description := ss.Description
		if page.Summary != nil && *page.Summary != "" {
			description = *page.Summary
		}
		m := docMeta{
			Title:       page.Title + " — " + ss.Title,
			Description: description,
			Canonical:   absURL(ss.BaseURL, "/"+slug),
			OGType:      "website",
			OGURL:       absURL(ss.BaseURL, "/"+slug),
			OGImage:     absURL(ss.BaseURL, ss.DefaultOGImage),
			SiteName:    ss.Title,
		}
		return renderMeta(m), http.StatusOK
	}
}

// writeDocument writes the SPA shell with the injected meta fragment. When the
// shell split failed (shellHead nil) it serves the raw embedded shell instead.
func (s *Server) writeDocument(w http.ResponseWriter, status int, meta string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if s.shellHead == nil {
		w.WriteHeader(status)
		_, _ = w.Write(s.index)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(s.shellHead)
	_, _ = io.WriteString(w, meta)
	_, _ = w.Write(s.shellTail)
}
