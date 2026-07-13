package enrich

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	readability "github.com/go-shiori/go-readability"
	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/store"
)

var urlRe = regexp.MustCompile(`https?://[^\s]+`)

func FirstURL(text string) string { return urlRe.FindString(text) }

func Extract(body io.Reader, pageURL string) (string, string, error) {
	u, _ := url.Parse(pageURL)
	art, err := readability.FromReader(body, u)
	if err != nil {
		return "", "", err
	}
	return art.Title, art.TextContent, nil
}

type Enrichment struct {
	LinkURL string
	Title   string
	Text    string
	Status  string // "ok" or "failed"
}

func Run(s *store.Store, hc *http.Client, emb *embed.Client, limit int) (int, int, error) {
	items, err := s.PendingEnrichment(limit)
	if err != nil {
		return 0, 0, err
	}
	var done, failed int
	for _, it := range items {
		link := FirstURL(it.Text)
		if link == "" {
			link = it.URL
		}
		e := Enrichment{LinkURL: link, Status: "failed"}

		resp, err := hc.Get(link)
		if err == nil && resp.StatusCode == 200 {
			if title, text, xerr := Extract(resp.Body, link); xerr == nil {
				e.Title, e.Text, e.Status = title, text, "ok"
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		if e.Status == "ok" {
			done++
		} else {
			failed++
		}
		if err := s.SaveEnrichment(it.ID, store.Enrichment{
			LinkURL: e.LinkURL, Title: e.Title, Text: e.Text, Status: e.Status,
		}); err != nil {
			return done, failed, err
		}
		if e.Status == "ok" && emb != nil {
			text := e.Title + "\n" + e.Text
			if len(text) > 2000 {
				text = text[:2000]
			}
			if vecs, verr := emb.Embed(context.Background(), []string{text}); verr == nil && len(vecs) > 0 {
				_ = s.SaveEmbedding(it.ID, emb.Model, vecs[0])
			}
		}
	}
	return done, failed, nil
}

// DefaultClient is a link fetcher that gives up quickly on dead links.
func DefaultClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }
