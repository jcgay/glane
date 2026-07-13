package twitter

import (
	"os"
	"testing"
)

func TestParseLikes(t *testing.T) {
	data, _ := os.ReadFile("testdata/like.js")
	items, err := ParseLikes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1, got %d", len(items))
	}
	got := items[0]
	if got.SourceID != "1590534767501398021" || got.Kind != "like" || got.Source != "twitter" {
		t.Fatalf("bad item: %+v", got)
	}
	if got.Text == "" {
		t.Fatal("empty text")
	}
}

func TestParseTweetsDetectsRepost(t *testing.T) {
	data, _ := os.ReadFile("testdata/tweets.js")
	items, err := ParseTweets(data)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Kind != "repost" {
		t.Fatalf("want repost, got %q", items[0].Kind)
	}
	if items[0].CreatedAt == 0 {
		t.Fatal("created_at not parsed")
	}
}
