package mover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const retrySrc = `package foo

func FilterA() int { return 1 }

func Other() int { return 2 }
`

// TestRunRetriesOnConflict simulates a sibling writer landing between op
// and commit: attempt 1 must detect the conflict and write nothing, attempt
// 2 re-runs against the fresh content and succeeds.
func TestRunRetriesOnConflict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	if err := os.WriteFile(src, []byte(retrySrc), 0o600); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	cfg := Config{
		Source: src,
		Sink:   filepath.Join(dir, "filter.go"),
		Regex:  "^Filter",
		Move:   true,
		testHookBeforeCommit: func() {
			if attempts == 0 {
				mutated := retrySrc + "\nfunc Extra() int { return 3 }\n"
				if err := os.WriteFile(src, []byte(mutated), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			attempts++
		},
	}
	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts: got %d want 2", attempts)
	}
	got, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		t.Fatal(err)
	}
	// The retry parsed the mutated source: Extra must survive the move.
	for _, want := range []string{"func Other", "func Extra"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("source lost %q after retry:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "FilterA") {
		t.Errorf("FilterA not moved out of source:\n%s", got)
	}
	sink, err := os.ReadFile(filepath.Clean(cfg.Sink))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sink), "func FilterA") {
		t.Errorf("sink missing FilterA:\n%s", sink)
	}
	if len(res.Matched) != 1 || res.Matched[0] != "FilterA" {
		t.Errorf("matched: %v", res.Matched)
	}
}

// TestRunConflictExhaustsRetries mutates the source on every attempt; Run
// must give up after Retries re-runs and surface the conflict.
func TestRunConflictExhaustsRetries(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	if err := os.WriteFile(src, []byte(retrySrc), 0o600); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	cfg := Config{
		Source:  src,
		Sink:    filepath.Join(dir, "filter.go"),
		Regex:   "^Filter",
		Move:    true,
		Retries: 2,
		testHookBeforeCommit: func() {
			attempts++
			mutated := fmt.Sprintf("%s\nfunc Extra%d() int { return 3 }\n", retrySrc, attempts)
			if err := os.WriteFile(src, []byte(mutated), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	_, err := Run(cfg)
	if !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	if attempts != 3 { // first attempt + 2 retries
		t.Fatalf("attempts: got %d want 3", attempts)
	}
	if _, statErr := os.Stat(cfg.Sink); !os.IsNotExist(statErr) {
		t.Fatal("exhausted run must not have written the sink")
	}
}
