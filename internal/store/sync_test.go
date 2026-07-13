package store

import (
	"path/filepath"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Unknown source → empty, no error.
	got, err := s.GetCursor("github")
	if err != nil || got != "" {
		t.Fatalf("want empty cursor, got %q err %v", got, err)
	}

	if err := s.SetCursor("github", "2023-01-15T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetCursor("github")
	if err != nil || got != "2023-01-15T10:00:00Z" {
		t.Fatalf("round-trip failed: got %q err %v", got, err)
	}

	// Overwrite.
	if err := s.SetCursor("github", "2024-02-20T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCursor("github")
	if got != "2024-02-20T00:00:00Z" {
		t.Fatalf("overwrite failed: got %q", got)
	}
}
