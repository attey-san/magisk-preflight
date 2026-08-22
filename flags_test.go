package main

import (
	"flag"
	"testing"
)

// newFlagSetForTest mirrors the lint flag set without touching os.Args.
func newFlagSetForTest() (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	jsonOn := fs.Bool("json", false, "emit JSON")
	return fs, jsonOn
}

// Flags must be accepted before, between and after positional arguments.
// flag's own loop stops at the first non-flag word, so without parseTrailing
// "lint mod --json" treated --json as a second path and failed.
func TestFlagPositions(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantSet bool
	}{
		{"flag before path", []string{"--json", "mod"}, true},
		{"flag after path", []string{"mod", "--json"}, true},
		{"no flags", []string{"mod"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, jsonOn := newFlagSetForTest()
			got := parseTrailing(fs, tc.args, 1)
			if got == nil {
				t.Fatalf("parseTrailing(%v) failed", tc.args)
			}
			if got[0] != "mod" {
				t.Errorf("got path %q, want mod", got[0])
			}
			if *jsonOn != tc.wantSet {
				t.Errorf("json flag set = %v, want %v", *jsonOn, tc.wantSet)
			}
		})
	}
}

func TestParseTrailingRejectsWrongCount(t *testing.T) {
	fs, _ := newFlagSetForTest()
	if got := parseTrailing(fs, []string{"a", "b"}, 1); got != nil {
		t.Errorf("two paths should fail, got %v", got)
	}
	fs, _ = newFlagSetForTest()
	if got := parseTrailing(fs, nil, 1); got != nil {
		t.Errorf("zero paths should fail, got %v", got)
	}
}

func TestParseTrailingStopsAtDoubleDash(t *testing.T) {
	fs, jsonOn := newFlagSetForTest()
	got := parseTrailing(fs, []string{"--", "--json"}, 1)
	if got == nil || got[0] != "--json" {
		t.Fatalf("-- must end flag parsing, got %v", got)
	}
	if *jsonOn {
		t.Error("--json after -- was parsed as a flag")
	}
}
