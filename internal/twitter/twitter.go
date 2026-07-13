package twitter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

// stripPrefix drops the "window.YTD.x.partN = " assignment, leaving the JSON array.
func stripPrefix(data []byte) []byte {
	if i := bytes.IndexByte(data, '='); i >= 0 {
		return bytes.TrimSpace(data[i+1:])
	}
	return data
}

func ParseLikes(data []byte) ([]store.Item, error) {
	var raw []struct {
		Like struct {
			TweetID     string `json:"tweetId"`
			FullText    string `json:"fullText"`
			ExpandedURL string `json:"expandedUrl"`
		} `json:"like"`
	}
	if err := json.Unmarshal(stripPrefix(data), &raw); err != nil {
		return nil, err
	}
	items := make([]store.Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, store.Item{
			Source: "twitter", SourceID: r.Like.TweetID, Kind: "like",
			Text: r.Like.FullText, URL: r.Like.ExpandedURL,
		})
	}
	return items, nil
}

func ParseTweets(data []byte) ([]store.Item, error) {
	var raw []struct {
		Tweet struct {
			IDStr     string `json:"id_str"`
			CreatedAt string `json:"created_at"`
			FullText  string `json:"full_text"`
		} `json:"tweet"`
	}
	if err := json.Unmarshal(stripPrefix(data), &raw); err != nil {
		return nil, err
	}
	items := make([]store.Item, 0, len(raw))
	for _, r := range raw {
		kind := "own"
		if strings.HasPrefix(r.Tweet.FullText, "RT @") {
			kind = "repost"
		}
		var ts int64
		if t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", r.Tweet.CreatedAt); err == nil {
			ts = t.Unix()
		}
		items = append(items, store.Item{
			Source: "twitter", SourceID: r.Tweet.IDStr, Kind: kind,
			Text: r.Tweet.FullText, CreatedAt: ts,
			URL: "https://twitter.com/i/web/status/" + r.Tweet.IDStr,
		})
	}
	return items, nil
}

func Import(s *store.Store, dir string) (int, int, error) {
	likeData, err := os.ReadFile(filepath.Join(dir, "data", "like.js"))
	if err != nil {
		return 0, 0, fmt.Errorf("read like.js: %w", err)
	}
	likes, err := ParseLikes(likeData)
	if err != nil {
		return 0, 0, err
	}
	if _, err := s.Upsert(likes); err != nil {
		return 0, 0, err
	}

	tweetData, err := os.ReadFile(filepath.Join(dir, "data", "tweets.js"))
	if err != nil {
		return len(likes), 0, fmt.Errorf("read tweets.js: %w", err)
	}
	tweets, err := ParseTweets(tweetData)
	if err != nil {
		return len(likes), 0, err
	}
	if _, err := s.Upsert(tweets); err != nil {
		return len(likes), 0, err
	}
	return len(likes), len(tweets), nil
}
