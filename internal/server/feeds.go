package server

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"strings"
	"time"
)

// sitemapURL is one <url> entry in the sitemap.
type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// urlset is the sitemap root element.
type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// handleSitemap serves /sitemap.xml. It is readiness-gated by the router and
// reads only the cache. Static routes (/, /resume, /blog) come first, then
// every published post. On a store error it returns 500.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	ss := s.siteSettings(r.Context())

	posts, err := s.store.SitemapPosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sitemap unavailable")
		return
	}

	set := urlset{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs,
		sitemapURL{Loc: absURL(ss.BaseURL, "/")},
		sitemapURL{Loc: absURL(ss.BaseURL, "/resume")},
		sitemapURL{Loc: absURL(ss.BaseURL, "/blog")},
	)
	for _, p := range posts {
		lastmod := p.PublishedAt
		if p.UpdatedAt != nil && *p.UpdatedAt != "" {
			lastmod = *p.UpdatedAt
		}
		set.URLs = append(set.URLs, sitemapURL{
			Loc:     absURL(ss.BaseURL, "/blog/"+p.Slug),
			LastMod: lastmod,
		})
	}

	body, err := xml.Marshal(set)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sitemap unavailable")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// handleRSS serves /rss.xml. It is readiness-gated by the router and reads only
// the cache. The feed is built manually so content:encoded and CDATA sections
// are emitted exactly as needed; items are the 50 newest published posts.
func (s *Server) handleRSS(w http.ResponseWriter, r *http.Request) {
	ss := s.siteSettings(r.Context())

	posts, err := s.store.FeedPosts(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "feed unavailable")
		return
	}

	// Build items first so lastBuildDate can reuse the newest item's pubDate.
	type rssItem struct {
		title, link, pubDate, summary, html string
	}
	items := make([]rssItem, 0, len(posts))
	var lastBuild string
	for _, p := range posts {
		link := absURL(ss.BaseURL, "/blog/"+p.Slug)
		var pubDate string
		if t, perr := time.Parse(time.RFC3339, p.PublishedAt); perr == nil {
			pubDate = t.Format(time.RFC1123Z)
		}
		if lastBuild == "" && pubDate != "" {
			lastBuild = pubDate
		}
		items = append(items, rssItem{
			title:   p.Title,
			link:    link,
			pubDate: pubDate,
			summary: p.Summary,
			html:    p.HTML,
		})
	}
	if lastBuild == "" {
		lastBuild = time.Now().Format(time.RFC1123Z)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">` + "\n")
	b.WriteString("<channel>\n")
	b.WriteString("<title>" + xmlEscape(ss.Title) + "</title>\n")
	b.WriteString("<link>" + xmlEscape(ss.BaseURL) + "</link>\n")
	b.WriteString("<description>" + xmlEscape(ss.Description) + "</description>\n")
	b.WriteString("<lastBuildDate>" + xmlEscape(lastBuild) + "</lastBuildDate>\n")
	for _, it := range items {
		b.WriteString("<item>\n")
		b.WriteString("<title>" + xmlEscape(it.title) + "</title>\n")
		b.WriteString("<link>" + xmlEscape(it.link) + "</link>\n")
		b.WriteString(`<guid isPermaLink="true">` + xmlEscape(it.link) + "</guid>\n")
		if it.pubDate != "" {
			b.WriteString("<pubDate>" + xmlEscape(it.pubDate) + "</pubDate>\n")
		}
		b.WriteString("<description>" + cdata(it.summary) + "</description>\n")
		b.WriteString("<content:encoded>" + cdata(it.html) + "</content:encoded>\n")
		b.WriteString("</item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// handleRobots serves /robots.txt. It is NOT readiness-gated: crawlers should
// always get a valid policy. The Sitemap line is emitted only when a base URL
// is known (settings baseUrl, else configBaseURL).
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	ss := s.siteSettings(r.Context())

	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	if ss.BaseURL != "" {
		b.WriteString("Sitemap: " + strings.TrimRight(ss.BaseURL, "/") + "/sitemap.xml\n")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// xmlEscape returns s with XML metacharacters escaped via encoding/xml.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// cdata wraps s in a CDATA section, splitting any literal "]]>" so it cannot
// terminate the section early.
func cdata(s string) string {
	s = strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + s + "]]>"
}
