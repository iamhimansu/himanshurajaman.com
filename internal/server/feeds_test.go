package server_test

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSitemap_PublishedOnly_WellFormed(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var set struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &set); err != nil {
		t.Fatalf("sitemap is not well-formed XML: %v", err)
	}
	if len(set.URLs) == 0 {
		t.Fatal("sitemap has no URLs")
	}

	joined := rr.Body.String()
	if !strings.Contains(joined, "https://example.test/blog/hello-world") {
		t.Error("sitemap missing a published post URL")
	}
	if strings.Contains(joined, "draft-note") {
		t.Error("sitemap must exclude drafts")
	}
	for _, u := range set.URLs {
		if !strings.HasPrefix(u.Loc, "https://example.test") {
			t.Errorf("sitemap URL is not absolute: %q", u.Loc)
		}
	}
}

func TestSitemap_IncludesPublishedPages(t *testing.T) {
	// AC-31: the sitemap lists published pages (at /:slug) but not draft pages.
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<loc>https://example.test/dsa</loc>") {
		t.Errorf("sitemap missing the published page /dsa:\n%s", body)
	}
	if strings.Contains(body, "/scratch") {
		t.Error("sitemap must exclude the draft page /scratch")
	}
}

func TestRSS_PublishedOnly_WellFormed(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/rss.xml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var doc struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []struct {
				Link string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("rss is not well-formed XML: %v", err)
	}
	if len(doc.Channel.Items) == 0 {
		t.Fatal("rss has no items")
	}
	for _, it := range doc.Channel.Items {
		if !strings.HasPrefix(it.Link, "https://example.test") {
			t.Errorf("rss link is not absolute: %q", it.Link)
		}
		if strings.Contains(it.Link, "draft-note") {
			t.Error("rss must exclude drafts")
		}
	}
}

func TestRobots_SitemapLine(t *testing.T) {
	h := newServer(t, options{})
	rr := h.serve(httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Sitemap: https://example.test/sitemap.xml") {
		t.Errorf("robots.txt missing sitemap line:\n%s", rr.Body.String())
	}
}
