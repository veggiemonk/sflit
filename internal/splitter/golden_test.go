// golden_test.go — byte-exact pins for the agent-facing contract surfaces:
// help text, --tool-schema, and the -json payload. Agents are routed
// through these (CLAUDE.md says "run sflit -h"), so drift an agent would
// see must fail a test, not just the substring fragments the contract
// tests check. Regenerate intentionally with:
//
//	go test ./internal/splitter/ -run TestGolden -update

package splitter

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden files with current output")

// goldenDir is resolved at init, before any test calls t.Chdir.
var goldenDir = func() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "testdata", "golden")
}()

// checkGolden compares got against the named golden file, rewriting it
// instead when -update is set.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output drifted from %s — intentional changes need -update\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestGoldenHelp(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)
	checkGolden(t, "help.txt", buf.Bytes())
}

func TestGoldenToolSchema(t *testing.T) {
	checkGolden(t, "tool_schema.json", toolSchemaJSON())
}

// TestGoldenJSONMove pins the full -json success payload through the real
// CLI. Relative paths via t.Chdir keep the payload machine-independent.
func TestGoldenJSONMove(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc FilterA() int { return 1 }\n\nfunc FilterB() int { return 2 }\n\nfunc Other() int { return 3 }\n"
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"-source", "big.go", "-sink", "filter.go", "-regex", "^Filter", "-move", "-json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	checkGolden(t, "json_move.json", stdout.Bytes())
}
