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
		return "", fmt.Errorf("Bluesky auth failed (check GLANE_BLUESKY_APP_PASSWORD)")
	}
	var out struct {
		AccessJwt string `json:"accessJwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessJwt, nil
}

type likesResp struct {
	Cursor string `json:"cursor"`
	Feed   []struct {
		Post struct {
			URI    string `json:"uri"`
			Author struct {
				Handle string `json:"handle"`
			} `json:"author"`
			Record struct {
				Text      string `json:"text"`
				CreatedAt string `json:"createdAt"`
			} `json:"record"`
		} `json:"post"`
	} `json:"feed"`
}

// permalink turns an AT-URI (at://did/app.bsky.feed.post/<rkey>) into a bsky.app link.
func permalink(handle, uri string) string {
	parts := strings.Split(uri, "/")
	rkey := parts[len(parts)-1]
	return "https://bsky.app/profile/" + handle + "/post/" + rkey
}

func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error) {
	jwt, err := createSession(handle, appPassword, hc)
	if err != nil {
		return 0, err
	}
	cursor, err := s.GetCursor("bluesky:likes")
	if err != nil {
		return 0, err
	}

	var items []store.Item
	newest := ""
	pageCursor := ""

	for {
		reqURL := fmt.Sprintf("%s/xrpc/app.bsky.feed.getActorLikes?actor=%s&limit=100", pdsBase, url.QueryEscape(handle))
		if pageCursor != "" {
			reqURL += "&cursor=" + pageCursor
		}
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, fmt.Errorf("Bluesky API status %d", resp.StatusCode)
		}
		var lr likesResp
		derr := json.NewDecoder(resp.Body).Decode(&lr)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(lr.Feed) == 0 {
			break
		}

		stop := false
		for _, f := range lr.Feed {
			p := f.Post
			if cursor != "" && p.URI == cursor {
				stop = true
				break
			}
			if newest == "" {
				newest = p.URI // newest-first: first seen this run is the newest
			}
			var ts int64
			if t, terr := time.Parse(time.RFC3339, p.Record.CreatedAt); terr == nil {
				ts = t.Unix()
			}
			// ponytail: only record.text is captured; external links live in record.embed
			// (app.bsky.embed.external.uri), so enrich falls back to the bsky.app permalink
			// for link posts. Capture embed.external.uri here if Bluesky enrichment matters.
			items = append(items, store.Item{
				Source:    "bluesky",
				SourceID:  p.URI,
				Kind:      "like",
				Author:    p.Author.Handle,
				Text:      p.Record.Text,
				URL:       permalink(p.Author.Handle, p.URI),
				CreatedAt: ts,
			})
		}
		if stop || lr.Cursor == "" {
			break
		}
		pageCursor = lr.Cursor
	}

	// ponytail: accumulates all new items in memory before one Upsert; fine for a
	// personal account. Stream per-page if volumes ever grow large.
	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != "" && newest != cursor {
		if err := s.SetCursor("bluesky:likes", newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}
