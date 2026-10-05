package web

import "testing"

func TestParseQuery(t *testing.T) {
	for _, tc := range []struct {
		q, source, tag, since string
		want                  query
	}{
		{"cold start", "", "", "", query{Words: "cold start"}},
		{"cold source:twitter start tag:aws since:30d", "", "", "", query{"cold start", "twitter", "aws", "30d"}},
		// typed operators win over the URL parameters, an empty one clears
		{"x source:github since:", "twitter", "go", "2024", query{"x", "github", "go", ""}},
		{"foo:bar", "", "", "", query{Words: "foo:bar"}},
		{`x tag:"software engineering" y`, "", "", "", query{Words: "x y", Tag: "software engineering"}},
	} {
		if got := parseQuery(tc.q, tc.source, tc.tag, tc.since); got != tc.want {
			t.Errorf("parseQuery(%q) = %+v, want %+v", tc.q, got, tc.want)
		}
	}
}

func TestQueryCLI(t *testing.T) {
	for q, want := range map[query]string{
		{}: "glane search",
		{Words: "cold start", Source: "twitter", Since: "30d"}: "glane search cold start --source twitter --since 30d",
		{Words: `it's "c++"`, Tag: "go"}:                       `glane search 'it'\''s' '"c++"' --tag go`,
		{Words: "-foo bar", Tag: "software engineering"}:       `glane search --tag 'software engineering' -- -foo bar`,
		{Words: "=ls"}: `glane search '=ls'`,
	} {
		if got := q.CLI(); got != want {
			t.Errorf("%+v.CLI() = %s, want %s", q, got, want)
		}
	}
}
