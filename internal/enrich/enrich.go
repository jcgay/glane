package enrich

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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

func Run(s *store.Store, hc *http.Client, emb *embed.Client, limit int, progress ...func(string)) (int, int, error) {
	items, err := s.PendingEnrichment(limit)
	if err != nil {
		return 0, 0, err
	}
	var done, failed int
	report := func(string) {}
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	}
	for i, it := range items {
		// The useful link is source-dependent: for GitHub stars it's the repo page
		// (it.URL); for Twitter the real article link lives in the post text (the t.co).
		var link string
		if it.Source == "github" {
			link = it.URL
		} else {
			link = FirstURL(it.Text)
			if link == "" {
				link = it.URL
			}
		}
		host := link
		if u, perr := url.Parse(link); perr == nil && u.Host != "" {
			host = u.Host
		}
		report(fmt.Sprintf("enrich [%d/%d] %s…", i+1, len(items), host))
		e := Enrichment{LinkURL: link, Status: "failed"}

		resp, err := hc.Get(link)
		if err == nil && resp.StatusCode == 200 {
			// resp.Request.URL is the final URL after redirects, so this
			// un-shortens t.co (and any other shortener) for free.
			e.LinkURL = cleanURL(resp.Request.URL)
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
			text := cutRunes(e.Title+"\n"+e.Text, 2000)
			if vecs, verr := emb.Embed(context.Background(), []string{text}); verr == nil && len(vecs) > 0 {
				_ = s.SaveEmbedding(it.ID, emb.Model, vecs[0])
			}
		}
	}
	return done, failed, nil
}

// DefaultClient is a link fetcher that gives up quickly on dead links.
func DefaultClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

// trackingParams are query keys stripped from resolved links; utm_* is matched
// by prefix. Add here if a new tracker shows up rather than growing the blocklist prose.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "gclsrc": true, "dclid": true, "msclkid": true,
	"twclid": true, "yclid": true, "igshid": true, "mc_cid": true, "mc_eid": true,
	"ref_src": true, "ref_url": true, "cmpid": true,
}

// cleanURL returns u without tracking query params. Non-tracking params are
// kept — many links need their query to resolve to the right content.
func cleanURL(u *url.URL) string {
	q := u.Query()
	for k := range q {
		if trackingParams[k] || strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// cutRunes truncates s to at most n runes, never splitting a multibyte rune.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
