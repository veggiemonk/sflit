package splitter

import (
	"bytes"
	"strings"
	"testing"
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

func TestHelpListsAllVersionFlags(t *testing.T) {
	for _, want := range []string{"-v", "-version", "--version"} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("helpText missing %q", want)
		}
	}
}
