package main

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func newFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("paper-results-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func TestParseConfigPath(t *testing.T) {
	got, err := parseConfigPath(newFlagSet(), []string{"-config", "/tmp/settings.json"})
	if err != nil || got != "/tmp/settings.json" {
		t.Fatalf("parseConfigPath = %q, %v", got, err)
	}
	if got, err := parseConfigPath(newFlagSet(), nil); err != nil || got != "" {
		t.Fatalf("no args: %q, %v", got, err)
	}
}

// TestParseConfigPath_RejectsStrayWords: flag stops at the first non-flag word,
// so "junk -config x" would otherwise start with the default settings file.
func TestParseConfigPath_RejectsStrayWords(t *testing.T) {
	for _, args := range [][]string{
		{"junk", "-config", "/tmp/settings.json"},
		{"-config", "/tmp/settings.json", "junk"},
	} {
		_, err := parseConfigPath(newFlagSet(), args)
		if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("%v: err = %v, want unexpected argument", args, err)
		}
	}
}
