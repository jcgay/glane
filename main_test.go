package main

import (
	"reflect"
	"testing"
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
