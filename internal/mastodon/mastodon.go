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
	Reblog *status `json:"reblog"`
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

func syncStream(s *store.Store, url, token, cursorKey string, hc *http.Client, mapItem func(status) store.Item, label string, report func(string)) (int, error) {
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
			return 0, fmt.Errorf("Mastodon auth failed (check MASTODON_ACCESS_TOKEN)")
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
			items = append(items, mapItem(st))
		}
		report(fmt.Sprintf("%s… %d", label, len(items)))
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

// Sync imports Mastodon favourites (kind "like"), bookmarks (kind "bookmark"),
// and the account's own feed: own posts (kind "own") and boosts (kind "repost",
// mapped to the reblogged status).
func Sync(s *store.Store, baseURL, token string, hc *http.Client, progress ...func(string)) (int, error) {
	report := func(string) {}
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	}

	baseURL = strings.TrimRight(baseURL, "/")
	like := func(st status) store.Item { return toItem(st, "like") }
	bookmark := func(st status) store.Item { return toItem(st, "bookmark") }
	author := func(st status) store.Item {
		if st.Reblog != nil {
			return toItem(*st.Reblog, "repost")
		}
		return toItem(st, "own")
	}

	fav, err := syncStream(s, baseURL+"/api/v1/favourites?limit=40", token, "mastodon:favourites", hc, like, "mastodon: favourites", report)
	if err != nil {
		return fav, err
	}
	bm, err := syncStream(s, baseURL+"/api/v1/bookmarks?limit=40", token, "mastodon:bookmarks", hc, bookmark, "mastodon: bookmarks", report)
	if err != nil {
		return fav + bm, err
	}
	id, err := verifyCredentials(baseURL, token, hc)
	if err != nil {
		return fav + bm, err
	}
	af, err := syncStream(s, baseURL+"/api/v1/accounts/"+id+"/statuses?exclude_replies=true&limit=40", token, "mastodon:authorfeed", hc, author, "mastodon: my posts", report)
	if err != nil {
		return fav + bm + af, err
	}
	return fav + bm + af, nil
}

func verifyCredentials(baseURL, token string, hc *http.Client) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("Mastodon auth failed (check MASTODON_ACCESS_TOKEN)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Mastodon API status %d for verify_credentials", resp.StatusCode)
	}
	var acc struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return "", err
	}
	return acc.ID, nil
}
