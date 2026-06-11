package splitter

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/veggiemonk/sflit/internal/version"
)

func TestRunCLI_UsageErrorsExit2(t *testing.T) {
	tests := []struct {
		name string
		want string
		args []string
	}{
		{
			name: "missing source",
			args: []string{"-sink", "b.go", "-regex", "Foo"},
			want: "missing -source flag",
		},
		{name: "missing sink", args: []string{"-source", "a.go", "-regex", "Foo"}, want: "missing -sink flag"},
		{
			name: "missing selection",
			args: []string{"-source", "a.go", "-sink", "b.go"},
			want: "at least one of -regex or -receiver is required",
		},
		{
			name: "invalid regex",
			args: []string{"-source", "a.go", "-sink", "b.go", "-regex", "("},
			want: "invalid -regex",
		},
		{name: "unknown flag", args: []string{"-nonsense"}, want: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := RunCLI(tt.args, nil, &stdout, &stderr)
			if got != 2 {
				t.Fatalf("exit code = %d, want 2; stderr:\n%s", got, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestRunCLI_VersionFlags(t *testing.T) {
	for _, args := range [][]string{{"-v"}, {"-version"}, {"--version"}} {
		t.Run(args[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := RunCLI(args, nil, &stdout, &stderr)
			if got != 0 {
				t.Fatalf("exit code = %d, want 0; stderr:\n%s", got, stderr.String())
			}
			want := fmt.Sprintln(version.Get())
			if stdout.String() != want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunCLI_RuntimeErrorsExit1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := RunCLI([]string{"-source", "missing.go", "-sink", "b.go", "-regex", "Foo"}, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing.go") {
		t.Fatalf("stderr = %q, want missing source path", stderr.String())
	}
}

func TestRunCLI_NoMatchIncludesSourceAndSelection(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/a.go"
	sink := dir + "/b.go"
	writeFileForCLITest(t, source, "package p\nfunc Foo(){}\n")

	var stdout, stderr bytes.Buffer
	got := RunCLI([]string{"-source", source, "-sink", sink, "-regex", "Nope"}, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	want := `sflit: no declarations matched in ` + source + ` for -regex "Nope"`
	if strings.TrimSpace(stderr.String()) != want {
		t.Fatalf("stderr = %q, want %q", strings.TrimSpace(stderr.String()), want)
	}
}

func TestRunCLI_PackageMismatchIncludesPathsAndPackages(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/a.go"
	sink := dir + "/b.go"
	writeFileForCLITest(t, source, "package p\nfunc Foo(){}\n")
	writeFileForCLITest(t, sink, "package q\nfunc Bar(){}\n")

	var stdout, stderr bytes.Buffer
	got := RunCLI([]string{"-source", source, "-sink", sink, "-regex", "Foo"}, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	want := `sflit: sink ` + sink + ` has package "q", but source ` + source + ` has package "p"`
	if strings.TrimSpace(stderr.String()) != want {
		t.Fatalf("stderr = %q, want %q", strings.TrimSpace(stderr.String()), want)
	}
}

func TestRunCLI_CollisionIncludesSinkAndName(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/a.go"
	sink := dir + "/b.go"
	writeFileForCLITest(t, source, "package p\nfunc Foo(){}\n")
	writeFileForCLITest(t, sink, "package p\nfunc Foo(){}\n")

	var stdout, stderr bytes.Buffer
	got := RunCLI([]string{"-source", source, "-sink", sink, "-regex", "Foo", "-move"}, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	want := `sflit: cannot write to ` + sink + `: declaration Foo already exists in sink`
	if strings.TrimSpace(stderr.String()) != want {
		t.Fatalf("stderr = %q, want %q", strings.TrimSpace(stderr.String()), want)
	}
}

func TestRunCLI_IdempotentMoveSecondRunNoMatch(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/a.go"
	sink := dir + "/b.go"
	writeFileForCLITest(t, source, "package p\nfunc Foo(){}\nfunc Bar(){}\n")

	args := []string{"-source", source, "-sink", sink, "-regex", "Foo", "-move"}
	var stdout, stderr bytes.Buffer
	got := RunCLI(args, nil, &stdout, &stderr)
	if got != 0 {
		t.Fatalf("first exit code = %d, want 0; stderr:\n%s", got, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	got = RunCLI(args, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("second exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("second stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no declarations matched") {
		t.Fatalf("second stderr = %q, want no declarations matched", stderr.String())
	}
}

func TestRunCLI_SameDirCopyRejectedExit1(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/a.go"
	sink := dir + "/b.go"
	writeFileForCLITest(t, source, "package p\nfunc Foo(){}\n")

	var stdout, stderr bytes.Buffer
	got := RunCLI([]string{"-source", source, "-sink", sink, "-regex", "Foo"}, nil, &stdout, &stderr)
	if got != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", got, stderr.String())
	}
	for _, want := range []string{"cannot copy within the same directory", source, sink, "-move"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(sink); !os.IsNotExist(err) {
		t.Fatalf("sink %s should not have been created (stat err = %v)", sink, err)
	}
}

func writeFileForCLITest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHelpListsAllVersionFlags(t *testing.T) {
	for _, want := range []string{"-v", "-version", "--version"} {
		re := regexp.MustCompile(`(^|[\s,;()])` + regexp.QuoteMeta(want) + `($|[\s,;()])`)
		if !re.MatchString(helpText) {
			t.Fatalf("helpText missing distinct token %q", want)
		}
	}
}
