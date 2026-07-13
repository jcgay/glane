package main

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestSplitQueryArgs(t *testing.T) {
	cases := []struct {
		args      []string
		wantQuery string
		wantFlags []string
	}{
		{[]string{"lambda"}, "lambda", []string{}},
		{[]string{"lambda", "calculus", "--limit", "2"}, "lambda calculus", []string{"--limit", "2"}},
		{[]string{"cold start", "--source", "github"}, "cold start", []string{"--source", "github"}},
		{[]string{"--limit", "2"}, "", []string{"--limit", "2"}},
	}
	for _, c := range cases {
		gotQuery, gotFlags := splitQueryArgs(c.args)
		if gotQuery != c.wantQuery || !reflect.DeepEqual(gotFlags, c.wantFlags) {
			t.Errorf("splitQueryArgs(%v) = (%q, %v), want (%q, %v)",
				c.args, gotQuery, gotFlags, c.wantQuery, c.wantFlags)
		}
	}
}

func TestParseSince(t *testing.T) {
	if ts, err := parseSince(""); ts != 0 || err != nil {
		t.Errorf("parseSince(\"\") = (%d, %v), want (0, nil)", ts, err)
	}
	if ts, err := parseSince("2023"); ts <= 0 || err != nil {
		t.Errorf("parseSince(\"2023\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if ts, err := parseSince("2023-06-15"); ts <= 0 || err != nil {
		t.Errorf("parseSince(\"2023-06-15\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if _, err := parseSince("nope"); err == nil {
		t.Errorf("parseSince(\"nope\") = nil error, want an error")
	}
}

func TestCutRunes(t *testing.T) {
	got := cutRunes("café☕more", 5)
	if !utf8.ValidString(got) {
		t.Fatalf("cutRunes produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("cutRunes rune count = %d, want 5", n)
	}
	if got := cutRunes("hi", 5); got != "hi" {
		t.Fatalf("cutRunes short string: got %q, want %q", got, "hi")
	}
}
