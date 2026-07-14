package bluesky

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var pdsBase = "https://bsky.social"

func createSession(handle, appPassword string, hc *http.Client) (string, error) {
	body, _ := json.Marshal(map[string]string{"identifier": handle, "password": appPassword})
	req, err := http.NewRequest("POST", pdsBase+"/xrpc/com.atproto.server.createSession", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Bluesky auth failed (check BLUESKY_APP_PASSWORD)")
	}
	var out struct {
		AccessJwt string `json:"accessJwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessJwt, nil
}

// postView is the common Bluesky post shape across likes/bookmarks/author-feed.
type postView struct {
	URI    string `json:"uri"`
	Author struct {
		Handle string `json:"handle"`
	} `json:"author"`
	Record struct {
		Text      string `json:"text"`
		CreatedAt string `json:"createdAt"`
	} `json:"record"`
}

// ponytail: only record.text is captured; external links live in record.embed
// (app.bsky.embed.external.uri), so enrich falls back to the bsky.app permalink
// for link posts. Capture embed.external.uri here if Bluesky enrichment matters.
func (p postView) toItem(kind string) store.Item {
	var ts int64
	if t, err := time.Parse(time.RFC3339, p.Record.CreatedAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "bluesky",
		SourceID:  p.URI,
		Kind:      kind,
		Author:    p.Author.Handle,
		Text:      p.Record.Text,
		URL:       permalink(p.Author.Handle, p.URI),
		CreatedAt: ts,
	}
}

// permalink turns an AT-URI (at://did/app.bsky.feed.post/<rkey>) into a bsky.app link.
func permalink(handle, uri string) string {
	parts := strings.Split(uri, "/")
	rkey := parts[len(parts)-1]
	return "https://bsky.app/profile/" + handle + "/post/" + rkey
}

func getJSON(hc *http.Client, jwt, reqURL string, out any) error {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bluesky API status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// syncStream runs the stop-at-URI + upsert + cursor-advance loop for one stream.
// fetch(pageCursor) returns the page's items (newest-first; each item's SourceID
// is its post URI, used as the stop key) and the next page cursor ("" = end).
//
// ponytail: accumulates all new items in memory before one Upsert; fine for a
// personal account. Stream per-page if volumes ever grow large.
//
// ponytail: stops on exact SourceID (post URI) match — AT-URIs aren't ordered,
// so there's no range compare. If the exact post the cursor points at leaves the
// feed (you unlike/delete it), the stop never fires and the whole feed is
// re-scanned + re-upserted every run until a surviving item re-anchors the
// cursor. Idempotent and fine at personal scale; revisit if a feed grows huge.
func syncStream(s *store.Store, cursorKey string, fetch func(pageCursor string) ([]store.Item, string, error), label string, report func(string)) (int, error) {
	cursor, err := s.GetCursor(cursorKey)
	if err != nil {
		return 0, err
	}
	var all []store.Item
	newest := ""
	pageCursor := ""
	for {
		items, next, err := fetch(pageCursor)
		if err != nil {
			return 0, err
		}
		if len(items) == 0 {
			break
		}
		stop := false
		for _, it := range items {
			if cursor != "" && it.SourceID == cursor {
				stop = true
				break
			}
			if newest == "" {
				newest = it.SourceID // newest-first: first seen this run is the newest
			}
			all = append(all, it)
		}
		report(fmt.Sprintf("%s… %d", label, len(all)))
		if stop || next == "" {
			break
		}
		pageCursor = next
	}
	if len(all) > 0 {
		if _, err := s.Upsert(all); err != nil {
			return 0, err
		}
	}
	if newest != "" && newest != cursor {
		if err := s.SetCursor(cursorKey, newest); err != nil {
			return len(all), err
		}
	}
	return len(all), nil
}

func likesFetch(hc *http.Client, jwt, handle string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.feed.getActorLikes?actor=%s&limit=100", pdsBase, url.QueryEscape(handle))
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor string `json:"cursor"`
			Feed   []struct {
				Post postView `json:"post"`
			} `json:"feed"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Feed))
		for _, f := range out.Feed {
			items = append(items, f.Post.toItem("like"))
		}
		return items, out.Cursor, nil
	}
}

func bookmarksFetch(hc *http.Client, jwt string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.bookmark.getBookmarks?limit=100", pdsBase)
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor    string `json:"cursor"`
			Bookmarks []struct {
				Item struct {
					Type string `json:"$type"`
					postView
				} `json:"item"`
			} `json:"bookmarks"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Bookmarks))
		for _, b := range out.Bookmarks {
			if b.Item.Type != "app.bsky.feed.defs#postView" {
				continue // blockedPost / notFoundPost — not viewable
			}
			items = append(items, b.Item.postView.toItem("bookmark"))
		}
		return items, out.Cursor, nil
	}
}

func authorFeedFetch(hc *http.Client, jwt, handle string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.feed.getAuthorFeed?actor=%s&limit=100&filter=posts_no_replies", pdsBase, url.QueryEscape(handle))
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor string `json:"cursor"`
			Feed   []struct {
				Post   postView `json:"post"`
				Reason struct {
					Type string `json:"$type"`
				} `json:"reason"`
			} `json:"feed"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Feed))
		for _, f := range out.Feed {
			kind := "own"
			if f.Reason.Type == "app.bsky.feed.defs#reasonRepost" {
				kind = "repost"
			}
			items = append(items, f.Post.toItem(kind))
		}
		return items, out.Cursor, nil
	}
}

// Sync imports likes, then saved posts (bookmarks), then the author's own posts
// and reposts. Order matters: later streams win kind on a shared post URI
// (repost/own > bookmark > like) via Upsert's overwrite.
func Sync(s *store.Store, handle, appPassword string, hc *http.Client, progress ...func(string)) (int, error) {
	report := func(string) {}
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	}

	jwt, err := createSession(handle, appPassword, hc)
	if err != nil {
		return 0, err
	}
	streams := []struct {
		key   string
		fetch func(string) ([]store.Item, string, error)
		label string
	}{
		{"bluesky:likes", likesFetch(hc, jwt, handle), "bluesky: likes"},
		{"bluesky:bookmarks", bookmarksFetch(hc, jwt), "bluesky: saved"},
		{"bluesky:authorfeed", authorFeedFetch(hc, jwt, handle), "bluesky: my posts"},
	}
	total := 0
	for _, st := range streams {
		n, err := syncStream(s, st.key, st.fetch, st.label, report)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
