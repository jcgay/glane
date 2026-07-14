package mastodon

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var (
	// Block-level breaks become whitespace so words don't merge across
	// paragraphs; all other tags are removed with NO substitution so a URL
	// split across <span>s rejoins intact.
	breakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>`)
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	wsRe    = regexp.MustCompile(`\s+`)
	nextRe  = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)
)

func stripHTML(s string) string {
	s = breakRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func nextLink(linkHeader string) string {
	if m := nextRe.FindStringSubmatch(linkHeader); m != nil {
		return m[1]
	}
	return ""
}

// idNewer reports whether Mastodon status id `a` is newer than the cursor.
// Ids are stringified snowflake integers → numeric compare when both parse;
// otherwise stop only on an exact match (treat everything else as newer).
func idNewer(a, cursor string) bool {
	if cursor == "" {
		return true
	}
	ai, aerr := strconv.ParseInt(a, 10, 64)
	ci, cerr := strconv.ParseInt(cursor, 10, 64)
	if aerr == nil && cerr == nil {
		return ai > ci
	}
	return a != cursor
}

type status struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	Account   struct {
		Acct string `json:"acct"`
	} `json:"account"`
}

func toItem(st status, kind string) store.Item {
	var ts int64
	if t, err := time.Parse(time.RFC3339, st.CreatedAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "mastodon",
		SourceID:  st.ID,
		Kind:      kind,
		Author:    st.Account.Acct,
		Text:      stripHTML(st.Content),
		URL:       st.URL,
		CreatedAt: ts,
	}
}

func syncStream(s *store.Store, url, token, kind, cursorKey string, hc *http.Client) (int, error) {
	cursor, err := s.GetCursor(cursorKey)
	if err != nil {
		return 0, err
	}
	var items []store.Item
	newest := cursor

	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return 0, fmt.Errorf("Mastodon auth failed (check GLANE_MASTODON_TOKEN)")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, fmt.Errorf("Mastodon API status %d for %s", resp.StatusCode, cursorKey)
		}
		var statuses []status
		next := nextLink(resp.Header.Get("Link"))
		derr := json.NewDecoder(resp.Body).Decode(&statuses)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(statuses) == 0 {
			break
		}
		stop := false
		for _, st := range statuses {
			if !idNewer(st.ID, cursor) {
				stop = true
				break
			}
			if idNewer(st.ID, newest) {
				newest = st.ID
			}
			items = append(items, toItem(st, kind))
		}
		if stop {
			break
		}
		url = next
	}

	// ponytail: accumulates all new items in memory before one Upsert; fine for a
	// personal account. Stream per-page if volumes ever grow large.
	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != cursor {
		if err := s.SetCursor(cursorKey, newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}

// Sync imports Mastodon favourites (kind "like") and bookmarks (kind "bookmark").
func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	fav, err := syncStream(s, baseURL+"/api/v1/favourites?limit=40", token, "like", "mastodon:favourites", hc)
	if err != nil {
		return fav, err
	}
	bm, err := syncStream(s, baseURL+"/api/v1/bookmarks?limit=40", token, "bookmark", "mastodon:bookmarks", hc)
	if err != nil {
		return fav + bm, err
	}
	return fav + bm, nil
}
