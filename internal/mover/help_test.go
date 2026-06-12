package mover

import (
	"flag"
	"io"
	"strconv"
	"strings"
	"testing"
)

// TestHelpText_FlagsAppearInOutput pins helpText to the real flag set:
// every flag registered by cliFlagSet must appear in the help output
// as "-<name>". helpText is the README source, so drift ships to docs.
func TestHelpText_FlagsAppearInOutput(t *testing.T) {
	var cfg Config
	var jsonOutput, debug bool
	fs := cliFlagSet(io.Discard, &cfg, &jsonOutput, &debug)

	flags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { flags[f.Name] = true })

	for name := range flags {
		if !strings.Contains(helpText, "-"+name) {
			t.Errorf("flag -%s is not mentioned in helpText", name)
		}
	}
}

// TestHelpText_DefaultsMatchFlagSet pins non-empty, non-"false" defaults
// declared in the flag set to the help text, so a default changed in one
// place (e.g. defaultRetries) cannot silently diverge from documentation.
func TestHelpText_DefaultsMatchFlagSet(t *testing.T) {
	var cfg Config
	var jsonOutput, debug bool
	fs := cliFlagSet(io.Discard, &cfg, &jsonOutput, &debug)

	fs.VisitAll(func(f *flag.Flag) {
		// Skip flags with empty or false defaults (default-false bools).
		if f.DefValue == "" || f.DefValue == "false" {
			return
		}

		if !strings.Contains(helpText, f.DefValue) {
			t.Errorf("flag -%s has default %q not mentioned in helpText", f.Name, f.DefValue)
		}
	})
}

// TestHelpText_RetriesDefaultEqualsConstant pins the "16" occurrences in
// helpText to the actual defaultRetries constant. Ensures that if the
// retry bound changes, the help text is updated to match, avoiding a drift
// between documented and actual behavior.
func TestHelpText_RetriesDefaultEqualsConstant(t *testing.T) {
	defaultStr := strconv.Itoa(defaultRetries)
	count := strings.Count(helpText, defaultStr)
	if count == 0 {
		t.Fatalf("helpText does not mention defaultRetries value %q", defaultStr)
	}
	// Expect "16" to appear at least twice: once for the actual default,
	// once for the ~16 concurrent movers guidance.
	if count < 2 {
		t.Errorf("helpText mentions defaultRetries %q only %d time(s), want >= 2", defaultStr, count)
	}
}
