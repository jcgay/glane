package web

import (
	"regexp"
	"strings"
)

// query is one /search request: the words that go to the search engines plus
// the filters, which the search box spells as source:, tag: and since: so it
// speaks the same language as `glane search --source --tag --since`.
type query struct {
	Words, Source, Tag, Since string
}

// ops matches one filter in the box; a value with spaces is double-quoted
// (tag:"software engineering"). index.html's OPS is the same pattern.
var ops = regexp.MustCompile(`(^|\s)(source|tag|since):("[^"]*"|\S*)`)

// parseQuery reads the filters from the URL parameters first, then lets any
// operator typed in q override them. An operator with an empty value (since:)
// clears that filter; an unknown one (foo:bar) is just a word.
func parseQuery(q, source, tag, since string) query {
	out := query{Source: source, Tag: tag, Since: since}
	for _, m := range ops.FindAllStringSubmatch(q, -1) {
		val := strings.Trim(m[3], `"`)
		switch m[2] {
		case "source":
			out.Source = val
		case "tag":
			out.Tag = val
		case "since":
			out.Since = val
		}
	}
	out.Words = strings.Join(strings.Fields(ops.ReplaceAllString(q, " ")), " ")
	return out
}

// CLI is the `glane search` invocation that returns the same listing, ready to
// paste in a shell. A word starting with "-" would end the query and be read
// as a flag, so then the flags go first and "--" ends them.
func (q query) CLI() string {
	var words, flags []string
	dash := false
	for _, w := range strings.Fields(q.Words) {
		words = append(words, shellQuote(w))
		dash = dash || strings.HasPrefix(w, "-")
	}
	for _, f := range [][2]string{{"--source", q.Source}, {"--tag", q.Tag}, {"--since", q.Since}} {
		if f[1] != "" {
			flags = append(flags, f[0], shellQuote(f[1]))
		}
	}
	parts := []string{"glane search"}
	if dash {
		parts = append(append(parts, flags...), "--")
		flags = nil
	}
	return strings.Join(append(append(parts, words...), flags...), " ")
}

var shellSafe = regexp.MustCompile(`^[\w./@:+,-]+$`) // no =: zsh expands a leading =cmd

// shellQuote single-quotes s unless every byte is safe bare in a POSIX shell.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
