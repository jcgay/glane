package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var (
	apiBase = "https://api.github.com"
	perPage = 100
)

type starEntry struct {
	StarredAt string `json:"starred_at"`
	Repo      struct {
		ID          int64  `json:"id"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repo"`
}

func toItem(e starEntry) store.Item {
	text := e.Repo.FullName
	if e.Repo.Description != "" {
		text += " — " + e.Repo.Description
	}
	var ts int64
	if t, err := time.Parse(time.RFC3339, e.StarredAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "github",
		SourceID:  strconv.FormatInt(e.Repo.ID, 10),
		Kind:      "star",
		Author:    e.Repo.Owner.Login,
		Text:      text,
		URL:       e.Repo.HTMLURL,
		CreatedAt: ts,
	}
}

// Sync pages the token owner's starred repos newest-first, upserts everything
// newer than the stored cursor, and advances the cursor only after success.
func Sync(s *store.Store, token string, hc *http.Client, progress ...func(string)) (int, error) {
	report := func(string) {}
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	}

	cursor, err := s.GetCursor("github")
	if err != nil {
		return 0, err
	}

	var items []store.Item
	newest := cursor

	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/user/starred?sort=created&direction=desc&per_page=%d&page=%d",
			apiBase, perPage, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github.star+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return 0, fmt.Errorf("GitHub: 401 — GITHUB_TOKEN is invalid or expired")
		}
		if resp.StatusCode != http.StatusOK {
			remaining := resp.Header.Get("X-RateLimit-Remaining")
			reset := resp.Header.Get("X-RateLimit-Reset")
			resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden && remaining == "0" {
				return 0, fmt.Errorf("GitHub: 403 — rate limit exceeded (resets at unix %s); wait and re-run", reset)
			}
			return 0, fmt.Errorf("GitHub: API status %d — check GITHUB_TOKEN is valid and has read access", resp.StatusCode)
		}
		var entries []starEntry
		derr := json.NewDecoder(resp.Body).Decode(&entries)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(entries) == 0 {
			break
		}

		stop := false
		for _, e := range entries {
			if cursor != "" && e.StarredAt <= cursor {
				stop = true
				break
			}
			if e.StarredAt > newest {
				newest = e.StarredAt
			}
			items = append(items, toItem(e))
		}
		report(fmt.Sprintf("github: stars… %d", len(items)))
		if stop || len(entries) < perPage {
			break
		}
	}

	// ponytail: full backfill accumulates all new stars in memory before one Upsert;
	// fine for a personal account. Stream per-page if star counts ever grow large.
	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != cursor {
		if err := s.SetCursor("github", newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}
